//! agent_bridge_v1 (gorge sb-arena reconstruction): serves the SpellBench
//! protocol v1 ENVIRONMENT role over stdin/stdout NDJSON on top of the
//! public in-process `RlEpisodeSessionV1` (policy surface V5, legal scan
//! answers only).
//!
//! Jack's own `agent_bridge_v1` (the one the published pauper-kernel
//! leaderboard used) is not in the public mtg-kernel tree. This is a
//! clean-room rebuild from `spec/SPELLBENCH_PROTOCOL_V1.md`: the kernel's
//! `ActionSemanticV1` maps one-to-one onto the v1 candidate kinds, the
//! decision's `state_summary` is projected from `ObservationV5`, and the
//! decision carries `x_kernel_v5` (`observation_json`, `legal_actions_json`)
//! exactly as the spec's section 9 describes the reference bridge doing.
//!
//! Local only; not upstreamed.
//!
//! Flags: `--no-x-kernel-v5` omits the extension (faster for bots that do
//! not read it). `--x-kernel-flat-v4` is refused (the Phase 1 flat encoder
//! bridge contract is private).

use mtg_kernel::card_def::KERNEL_CARDDB_HASH;
use mtg_kernel::engine::CastMode;
use mtg_kernel::mana::ManaColor;
use mtg_kernel::rl::{
    card_name, ActionSemanticV1, CardStableRefV1, ObservationV5, PlayerSeatV1, TargetRefV1,
    TerminalClassificationV1, TerminalOutcomeV1, ZoneIndependentStepV1,
};
use mtg_kernel::rl_session::{
    RlEpisodeSessionV1, RlSessionDecisionV1, RlSessionResponseV1, RlSessionTerminalV1,
};
use mtg_kernel::state::Zone;
use mtg_kernel::KERNEL_VERSION;
use serde_json::{json, Map, Value};
use sha2::{Digest, Sha256};
use std::collections::HashSet;
use std::io::{self, BufRead, Write};

const PROTOCOL: &str = "spellbench/v1";
const FORMAT: &str = "pauper-bo1";
const MAX_LINE: usize = 8 << 20;
const MAX_SAFE: u64 = 1 << 53;
const DECKS: [&str; 9] = [
    "Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "Terror", "CawGates", "Faeries",
];

struct Err {
    code: &'static str,
    message: String,
}

fn err(code: &'static str, message: impl Into<String>) -> Err {
    Err {
        code,
        message: message.into(),
    }
}

struct Current {
    decision: RlSessionDecisionV1,
    /// Wire semantic per candidate, `None` when the kernel action has no
    /// v1 kind (the decision is then halted before it is ever emitted).
    semantics: Vec<Value>,
}

struct Game {
    game_id: String,
    episode_id: u64,
    session: RlEpisodeSessionV1,
    current: Option<Current>,
    terminal: bool,
}

struct Bridge {
    emit_x_kernel_v5: bool,
    seen_ids: HashSet<String>,
    last: Option<(String, String, String)>, // (request_id, raw line, response)
    game: Option<Game>,
    episodes: u64,
    halts: u64,
}

fn engine_identity() -> Value {
    json!({
        "name": "mtg-kernel",
        "version": KERNEL_VERSION,
        "source_revision": option_env!("SB_BRIDGE_SOURCE_REVISION"),
        "rules_snapshot_id": format!("mtg-kernel-rules/{}/carddb-{:016x}", KERNEL_VERSION, KERNEL_CARDDB_HASH),
        "card_pool_identity": format!("mtg-kernel-pauper-pool-v1/carddb-{:016x}", KERNEL_CARDDB_HASH),
    })
}

fn provenance() -> Value {
    let e = engine_identity();
    json!({
        "engine_name": e["name"],
        "engine_version": e["version"],
        "rules_snapshot_id": e["rules_snapshot_id"],
        "card_pool_identity": e["card_pool_identity"],
    })
}

fn seat(p: PlayerSeatV1) -> &'static str {
    match p {
        PlayerSeatV1::P0 => "p0",
        PlayerSeatV1::P1 => "p1",
    }
}

fn zone(z: Zone) -> &'static str {
    match z {
        Zone::Library => "library",
        Zone::Hand => "hand",
        Zone::Battlefield => "battlefield",
        Zone::Graveyard => "graveyard",
        Zone::Stack => "stack",
        Zone::Exile => "exile",
        Zone::Command => "command",
    }
}

fn phase(step: ZoneIndependentStepV1) -> &'static str {
    use ZoneIndependentStepV1::*;
    match step {
        Untap => "untap",
        Upkeep => "upkeep",
        Draw => "draw",
        Main1 => "precombat_main",
        BeginCombat => "beginning_of_combat",
        DeclareAttackers => "declare_attackers",
        DeclareBlockers => "declare_blockers",
        CombatDamage => "combat_damage",
        EndCombat => "end_of_combat",
        Main2 => "postcombat_main",
        End => "end_step",
        Cleanup => "cleanup",
    }
}

fn snake(debug: &str) -> String {
    let mut out = String::new();
    for (i, ch) in debug.chars().enumerate() {
        if ch.is_ascii_uppercase() {
            if i > 0 {
                out.push('_');
            }
            out.push(ch.to_ascii_lowercase());
        } else {
            out.push(ch);
        }
    }
    out
}

/// Observer-relative object reference (spec section 5). A card in an
/// opponent's hand or any library is hidden unless the observation already
/// shows its identity to the acting seat.
fn obj(r: &CardStableRefV1, obs: &ObservationV5) -> Value {
    let hidden = match r.zone {
        Zone::Library => !obs
            .known_library_cards
            .iter()
            .flatten()
            .any(|k| k.card.stable.arena_id == r.arena_id),
        Zone::Hand => {
            r.owner != obs.acting_player
                && !obs
                    .known_hand_cards
                    .iter()
                    .flatten()
                    .any(|k| k.stable.arena_id == r.arena_id)
        }
        _ => false,
    };
    json!({
        "object_id": format!("obj-{:06}-{}", r.arena_id, r.zone_change_count),
        "card_name": if hidden { Value::Null } else { Value::String(card_name(r.card_db_id)) },
        "owner_seat": seat(r.owner),
        "controller_seat": seat(r.controller),
        "zone": zone(r.zone),
    })
}

fn target(t: &TargetRefV1, obs: &ObservationV5) -> Value {
    match t {
        TargetRefV1::Player { player } => json!({"player": seat(*player)}),
        TargetRefV1::Object { object } => json!({"object": obj(object, obs)}),
    }
}

fn mana(c: Option<ManaColor>) -> Value {
    match c {
        None => Value::Null,
        Some(c) => Value::String(format!("{c:?}")),
    }
}

fn color_name(c: ManaColor) -> Option<&'static str> {
    match c {
        ManaColor::W => Some("white"),
        ManaColor::U => Some("blue"),
        ManaColor::B => Some("black"),
        ManaColor::R => Some("red"),
        ManaColor::G => Some("green"),
        ManaColor::C => None,
    }
}

/// Maps one kernel action onto its v1 wire semantic, or explains why v1
/// cannot carry it.
fn semantic(s: &ActionSemanticV1, obs: &ObservationV5) -> Result<Value, String> {
    use ActionSemanticV1::*;
    Ok(match s {
        Pass { .. } => json!({"kind": "pass"}),
        PlayLand { source, .. } => json!({"kind": "play_land", "source": obj(source, obs)}),
        CastSpell { source, .. } => json!({"kind": "cast_spell", "source": obj(source, obs)}),
        ActivateManaAbility {
            source,
            mana_choice,
            cost_target,
            ..
        } => json!({
            "kind": "activate_mana_ability",
            "source": obj(source, obs),
            "mana_choice": mana(*mana_choice),
            "cost_target": match cost_target {
                None => Value::Null,
                Some(t) => json!({"object": obj(t, obs)}),
            },
        }),
        ActivateAbility {
            source,
            ability_index,
            ..
        } => json!({"kind": "activate_ability", "source": obj(source, obs), "ability_index": ability_index}),
        PlotSpell { source, .. } => json!({"kind": "plot_spell", "source": obj(source, obs)}),
        ChooseTarget {
            source,
            remaining,
            target: t,
            ..
        } => json!({"kind": "choose_target", "source": obj(source, obs), "remaining": remaining, "target": target(t, obs)}),
        ChooseCostTarget {
            source,
            cost_kind,
            remaining,
            candidate,
            ..
        } => json!({
            "kind": "choose_cost_target",
            "source": obj(source, obs),
            "cost_kind": snake(&format!("{:?}", cost_kind)),
            "remaining": remaining,
            "candidate": obj(candidate, obs),
        }),
        ChooseCastMode { source, mode, .. } => json!({
            "kind": "choose_cast_mode",
            "source": obj(source, obs),
            "mode": match mode { CastMode::Normal => "normal", CastMode::Alternative => "alternative" },
        }),
        ChooseKicker { source, pay, .. } => json!({"kind": "choose_kicker", "source": obj(source, obs), "pay": pay}),
        ChooseSpellMode {
            source,
            mode_index,
            mode_count,
            ..
        } => json!({"kind": "choose_spell_mode", "source": obj(source, obs), "mode_index": mode_index, "mode_count": mode_count}),
        ChooseEffectOption {
            source,
            option_index,
            option_count,
            ..
        } => json!({"kind": "choose_option", "source": obj(source, obs), "option_index": option_index, "option_count": option_count}),
        ChooseEffectTarget {
            source,
            target: t,
            selected_count,
            min_targets,
            max_targets,
            ..
        } => json!({
            "kind": "choose_effect_target",
            "source": obj(source, obs),
            "target": target(t, obs),
            "selected_count": selected_count,
            "min_targets": min_targets,
            "max_targets": max_targets,
        }),
        FinishEffectSelection {
            source,
            selected_count,
            ..
        } => json!({"kind": "finish_effect_selection", "source": obj(source, obs), "selected_count": selected_count}),
        ChooseEffectColor { source, color, .. } => match color_name(*color) {
            Some(name) => json!({"kind": "choose_color", "source": obj(source, obs), "color": name}),
            None => return Err("choose_color offered colorless".into()),
        },
        ChooseEffectNumber {
            source,
            number,
            minimum,
            maximum,
            ..
        } => json!({"kind": "choose_number", "source": obj(source, obs), "value": number, "minimum": minimum, "maximum": maximum}),
        ChooseEffectBoolean { source, value, .. } => json!({"kind": "choose_boolean", "source": obj(source, obs), "value": value}),
        FinishTargetSelection {
            source,
            selected_count,
            ..
        } => json!({"kind": "finish_target_selection", "source": obj(source, obs), "selected_count": selected_count}),
        ChooseOptionalCostUse { use_cost, .. } => json!({"kind": "choose_optional_cost_use", "use_cost": use_cost}),
        ChooseOptionalCostWhich { choice, .. } => json!({
            "kind": "choose_optional_cost_which",
            "choice": snake(&format!("{:?}", choice)),
        }),
        ChooseSpellCopyPayment { source, pay, .. } => json!({"kind": "choose_spell_copy_payment", "source": obj(source, obs), "pay": pay}),
        ChooseSpellCopyRetarget {
            source,
            change_target,
            ..
        } => json!({"kind": "choose_spell_copy_retarget", "source": obj(source, obs), "change_target": change_target}),
        ChooseMadnessCast { card, cast_it, .. } => json!({"kind": "choose_madness_cast", "card": obj(card, obs), "cast_it": cast_it}),
        Discard { cards, .. } => {
            if cards.len() != 1 {
                return Err(format!("discard of {} cards (v1 allows exactly one)", cards.len()));
            }
            json!({"kind": "discard", "cards": [obj(&cards[0], obs)]})
        }
        ChooseAttackerInclusion { attacker, include, .. } => json!({"kind": "choose_attacker_inclusion", "attacker": obj(attacker, obs), "include": include}),
        ChooseBlockerInclusion {
            attacker,
            blocker,
            include,
            ..
        } => json!({"kind": "choose_blocker_inclusion", "attacker": obj(attacker, obs), "blocker": obj(blocker, obs), "include": include}),
        OrderTriggers {
            pending_sources,
            order,
            ..
        } => json!({
            "kind": "order_triggers",
            "pending_sources": pending_sources.iter().map(|r| obj(r, obs)).collect::<Vec<_>>(),
            "order": order,
        }),
        DeclareAttackers { .. } => return Err("aggregate declare_attackers has no v1 kind".into()),
        DeclareBlockersForAttacker { .. } => return Err("aggregate declare_blockers has no v1 kind".into()),
        Ambiguous { reason } => return Err(format!("ambiguous action: {reason}")),
    })
}

fn canonical(v: &Value) -> String {
    // serde_json::Map is a BTreeMap here (no preserve_order), so keys are
    // emitted sorted by code point; compact separators are the default.
    serde_json::to_string(v).expect("json serializes")
}

fn candidates_sha256(cands: &[Value]) -> String {
    let reduced: Vec<Value> = cands
        .iter()
        .map(|c| json!({"candidate_id": c["candidate_id"], "semantic": c["semantic"]}))
        .collect();
    let digest = Sha256::digest(canonical(&Value::Array(reduced)).as_bytes());
    digest.iter().map(|b| format!("{b:02x}")).collect()
}

fn envelope(kind: &str, request_id: &str) -> Map<String, Value> {
    let mut m = Map::new();
    m.insert("response_type".into(), json!(kind));
    m.insert("protocol".into(), json!(PROTOCOL));
    m.insert("request_id".into(), json!(request_id));
    m
}

fn error_response(request_id: &str, e: Err) -> Value {
    let mut m = envelope("error", request_id);
    m.insert("error".into(), json!({"code": e.code, "message": e.message}));
    Value::Object(m)
}

fn req_str<'a>(m: &'a Map<String, Value>, key: &str) -> Result<&'a str, Err> {
    m.get(key)
        .and_then(Value::as_str)
        .ok_or_else(|| err("malformed_request", format!("{key} must be a string")))
}

fn req_u64(m: &Map<String, Value>, key: &str) -> Result<u64, Err> {
    match m.get(key).and_then(Value::as_u64) {
        Some(v) if v <= MAX_SAFE => Ok(v),
        _ => Err(err("malformed_request", format!("{key} must be an integer in [0, 2^53]"))),
    }
}

fn exact_keys(m: &Map<String, Value>, keys: &[&str]) -> Result<(), Err> {
    for k in m.keys() {
        if !keys.contains(&k.as_str()) {
            return Err(err("malformed_request", format!("unknown field {k:?}")));
        }
    }
    for k in keys {
        if !m.contains_key(*k) {
            return Err(err("malformed_request", format!("missing field {k:?}")));
        }
    }
    Ok(())
}

fn terminal_value(request_id: &str, game_id: &str, t: &RlSessionTerminalV1) -> Value {
    let (outcome, classification) = (
        match t.terminal_outcome {
            TerminalOutcomeV1::P0Win => "p0_win",
            TerminalOutcomeV1::P1Win => "p1_win",
            TerminalOutcomeV1::Draw => "draw",
            TerminalOutcomeV1::Truncated => "truncated",
            TerminalOutcomeV1::Halted => "halted",
        },
        match t.terminal_classification {
            TerminalClassificationV1::Natural => "natural",
            TerminalClassificationV1::Truncated => "truncated",
            TerminalClassificationV1::Halted => "halted",
        },
    );
    let winner = match (classification, t.winner) {
        ("natural", Some(w)) => json!(seat(w)),
        _ => Value::Null,
    };
    let mut m = envelope("terminal", request_id);
    m.insert("game_id".into(), json!(game_id));
    m.insert("outcome".into(), json!(outcome));
    m.insert("classification".into(), json!(classification));
    m.insert("winner".into(), winner);
    m.insert("reason".into(), json!(t.terminal_reason));
    m.insert("step_count".into(), json!(t.policy_step_count));
    m.insert("decision_count".into(), json!(t.physical_decision_count));
    m.insert("provenance".into(), provenance());
    Value::Object(m)
}

impl Bridge {
    fn halted(&mut self, request_id: &str, why: String) -> Value {
        self.halts += 1;
        eprintln!("agent_bridge_v1: HALT {why}");
        let game = self.game.as_mut().expect("halt inside a game");
        game.terminal = true;
        game.current = None;
        let mut m = envelope("terminal", request_id);
        m.insert("game_id".into(), json!(game.game_id));
        m.insert("outcome".into(), json!("halted"));
        m.insert("classification".into(), json!("halted"));
        m.insert("winner".into(), Value::Null);
        m.insert("reason".into(), json!(format!("bridge_halt: {why}")));
        m.insert("step_count".into(), json!(game.session.policy_step_count()));
        // Only completed groups count (section 8).
        m.insert(
            "decision_count".into(),
            json!(game.session.physical_decision_count()),
        );
        m.insert("provenance".into(), provenance());
        Value::Object(m)
    }

    /// Renders the session's current response as a v1 decision/terminal.
    fn render(&mut self, request_id: &str) -> Value {
        match self.render_inner(request_id) {
            Ok(v) => v,
            Err(why) => self.halted(request_id, why),
        }
    }

    fn render_inner(&mut self, request_id: &str) -> Result<Value, String> {
        let emit_x_kernel_v5 = self.emit_x_kernel_v5;
        let game = self.game.as_mut().expect("render inside a game");
        Ok(match game.session.current_response() {
            RlSessionResponseV1::Terminal(t) => {
                game.terminal = true;
                game.current = None;
                terminal_value(request_id, &game.game_id, &t)
            }
            RlSessionResponseV1::Decision(d) => {
                let mut semantics = Vec::with_capacity(d.legal_actions.len());
                for (i, a) in d.legal_actions.iter().enumerate() {
                    if a.selected_index as usize != i {
                        return Err(format!("legal action {i} has selected_index {}", a.selected_index));
                    }
                    match semantic(&a.semantic, &d.observation) {
                        Ok(v) => semantics.push(v),
                        Err(why) => return Err(why),
                    }
                }
                if semantics.is_empty() {
                    return Err("decision with no legal actions".into());
                }
                let candidates: Vec<Value> = d
                    .legal_actions
                    .iter()
                    .zip(&semantics)
                    .enumerate()
                    .map(|(i, (a, s))| {
                        json!({"candidate_id": i, "semantic": s, "display_text": a.display_text})
                    })
                    .collect();
                let p = &d.observation.projection.surface;
                let seats: Vec<Value> = [PlayerSeatV1::P0, PlayerSeatV1::P1]
                    .iter()
                    .enumerate()
                    .map(|(i, s)| {
                        json!({
                            "seat": seat(*s),
                            "life": p.life_totals[i],
                            "hand_count": p.hand_counts[i],
                            "library_count": p.library_counts[i],
                            "graveyard_count": p.graveyards[i].len(),
                            "battlefield_count": p.battlefield[i].len(),
                        })
                    })
                    .collect();
                let mut ext = Map::new();
                if emit_x_kernel_v5 {
                    ext.insert(
                        "x_kernel_v5".into(),
                        json!({
                            "observation_json": serde_json::to_string(&d.observation).expect("observation serializes"),
                            "legal_actions_json": serde_json::to_string(&d.legal_actions).expect("actions serialize"),
                        }),
                    );
                }
                let mut m = envelope("decision", request_id);
                m.insert("game_id".into(), json!(game.game_id));
                m.insert("step".into(), json!(d.step));
                m.insert("acting_seat".into(), json!(seat(d.acting_player)));
                m.insert(
                    "group".into(),
                    json!({"group_id": d.physical_decision_id, "substep_index": d.substep_index, "substep_count": d.substep_count}),
                );
                m.insert(
                    "state_summary".into(),
                    json!({
                        "turn": p.turn,
                        "phase_step": phase(p.phase),
                        "active_seat": seat(p.active_player),
                        "priority_seat": seat(p.priority_player),
                        "seats": seats,
                        "stack_count": p.stack.len(),
                    }),
                );
                m.insert("candidates_sha256".into(), json!(candidates_sha256(&candidates)));
                m.insert("candidates".into(), Value::Array(candidates));
                m.insert("provenance".into(), provenance());
                m.insert("extensions".into(), Value::Object(ext));
                game.current = Some(Current {
                    decision: d,
                    semantics,
                });
                Value::Object(m)
            }
        })
    }

    fn reset(&mut self, request_id: &str, m: &Map<String, Value>) -> Result<Value, Err> {
        exact_keys(
            m,
            &[
                "request_type", "protocol", "request_id", "game_id", "format", "seats",
                "game_seed", "max_decisions", "max_steps",
            ],
        )?;
        let game_id = req_str(m, "game_id")?.to_string();
        let format = req_str(m, "format")?;
        let seed = req_u64(m, "game_seed")?;
        let max_decisions = req_u64(m, "max_decisions")?;
        let max_steps = req_u64(m, "max_steps")?;
        if let Some(g) = &self.game {
            if !g.terminal {
                return Err(err("game_already_active", format!("game {:?} is active", g.game_id)));
            }
        }
        if format != FORMAT {
            return Err(err("unsupported_format", format!("format {format:?} is not served")));
        }
        let seats = m
            .get("seats")
            .and_then(Value::as_array)
            .filter(|a| a.len() == 2)
            .ok_or_else(|| err("malformed_request", "seats must be a 2-entry array"))?;
        let mut deck_ids = [String::new(), String::new()];
        for (i, s) in seats.iter().enumerate() {
            let s = s
                .as_object()
                .ok_or_else(|| err("malformed_request", "seat entry must be an object"))?;
            exact_keys(s, &["seat", "deck"])?;
            if s.get("seat").and_then(Value::as_str) != Some(["p0", "p1"][i]) {
                return Err(err("malformed_request", "seats must be ordered p0, p1"));
            }
            let deck = s
                .get("deck")
                .and_then(Value::as_object)
                .ok_or_else(|| err("malformed_request", "deck must be an object"))?;
            if deck.contains_key("decklist") {
                return Err(err("unsupported_deck", "decklists as data are not supported"));
            }
            exact_keys(deck, &["catalog_id"])?;
            let id = req_str(deck, "catalog_id")?;
            if !DECKS.contains(&id) {
                return Err(err("unsupported_deck", format!("unknown catalog_id {id:?}")));
            }
            deck_ids[i] = id.to_string();
        }
        self.episodes += 1;
        let session = RlEpisodeSessionV1::reset_with_decks_and_limits(
            self.episodes,
            seed,
            max_decisions.max(1),
            max_steps.max(1),
            deck_ids,
        )
        .map_err(|e| err("unsupported_deck", e.to_string()))?;
        self.game = Some(Game {
            game_id,
            episode_id: self.episodes,
            session,
            current: None,
            terminal: false,
        });
        Ok(self.render(request_id))
    }

    fn step(&mut self, request_id: &str, m: &Map<String, Value>) -> Result<Value, Err> {
        exact_keys(
            m,
            &["request_type", "protocol", "request_id", "game_id", "expected_step", "selection"],
        )?;
        let game_id = req_str(m, "game_id")?;
        let expected = req_u64(m, "expected_step")?;
        let selection = m
            .get("selection")
            .and_then(Value::as_object)
            .ok_or_else(|| err("malformed_request", "selection must be an object"))?;
        exact_keys(selection, &["candidate_id", "semantic_echo"])?;
        let cid = req_u64(selection, "candidate_id")?;
        let echo = &selection["semantic_echo"];
        let game = self
            .game
            .as_mut()
            .ok_or_else(|| err("step_before_reset", "no game has been reset"))?;
        if game.game_id != game_id {
            return Err(err("game_id_mismatch", format!("active game is {:?}", game.game_id)));
        }
        if game.terminal {
            return Err(err("game_already_terminal", "the game is over"));
        }
        let cur = game.current.as_ref().expect("live game has a decision");
        if expected != cur.decision.step {
            return Err(err(
                "expected_step_mismatch",
                format!("expected_step {expected} != step {}", cur.decision.step),
            ));
        }
        if cid as usize >= cur.semantics.len() {
            return Err(err(
                "candidate_id_out_of_range",
                format!("candidate_id {cid} >= {}", cur.semantics.len()),
            ));
        }
        if *echo != cur.semantics[cid as usize] {
            return Err(err("semantic_echo_mismatch", "semantic_echo differs from the candidate"));
        }
        let action = &cur.decision.legal_actions[cid as usize];
        let (index, stable) = (action.selected_index, action.stable_id.clone());
        let episode = game.episode_id;
        let step = cur.decision.step;
        match game.session.step(episode, step, index, &stable) {
            Ok(_) => Ok(self.render(request_id)),
            Err(e) => Ok(self.halted(request_id, format!("kernel refused step: {e}"))),
        }
    }

    fn handle(&mut self, line: &str) -> String {
        if line.len() > MAX_LINE {
            return canonical(&error_response("", err("malformed_request", "line exceeds 8 MiB")));
        }
        let value: Value = match serde_json::from_str(line) {
            Ok(v) => v,
            Err(e) => return canonical(&error_response("", err("malformed_json", e.to_string()))),
        };
        let Some(m) = value.as_object() else {
            return canonical(&error_response("", err("malformed_json", "top level must be an object")));
        };
        let Some(request_id) = m.get("request_id").and_then(Value::as_str) else {
            return canonical(&error_response("", err("malformed_request", "request_id must be a string")));
        };
        let request_id = request_id.to_string();
        if let Some((id, raw, resp)) = &self.last {
            if *id == request_id && raw == line {
                return resp.clone();
            }
        }
        if self.seen_ids.contains(&request_id) {
            return canonical(&error_response(
                &request_id,
                err("request_id_reuse_mismatch", "request_id reused with a different payload"),
            ));
        }
        let response = self.dispatch(&request_id, m);
        let out = canonical(&response);
        self.seen_ids.insert(request_id.clone());
        self.last = Some((request_id, line.to_string(), out.clone()));
        out
    }

    fn dispatch(&mut self, request_id: &str, m: &Map<String, Value>) -> Value {
        let result = (|| -> Result<Value, Err> {
            if m.get("protocol").and_then(Value::as_str) != Some(PROTOCOL) {
                return Err(err("protocol_mismatch", "protocol must be spellbench/v1"));
            }
            match req_str(m, "request_type")? {
                "hello" => {
                    exact_keys(m, &["request_type", "protocol", "request_id"])?;
                    let mut r = envelope("hello_ok", request_id);
                    r.insert("engine".into(), engine_identity());
                    r.insert("formats".into(), json!([FORMAT]));
                    r.insert("capabilities".into(), json!({"decklists_as_data": false}));
                    r.insert(
                        "extensions".into(),
                        if self.emit_x_kernel_v5 { json!(["x_kernel_v5"]) } else { json!([]) },
                    );
                    Ok(Value::Object(r))
                }
                "reset" => self.reset(request_id, m),
                "step" => self.step(request_id, m),
                other => Err(err("malformed_request", format!("unknown request_type {other:?}"))),
            }
        })();
        match result {
            Ok(v) => v,
            Err(e) => {
                eprintln!("agent_bridge_v1: error {}: {}", e.code, e.message);
                error_response(request_id, e)
            }
        }
    }
}

fn main() {
    let mut emit = true;
    for arg in std::env::args().skip(1) {
        match arg.as_str() {
            "--no-x-kernel-v5" => emit = false,
            "--x-kernel-v5" => emit = true,
            _ => {
                eprintln!("usage: agent_bridge_v1 [--no-x-kernel-v5]  (unsupported flag {arg:?})");
                std::process::exit(2);
            }
        }
    }
    let mut bridge = Bridge {
        emit_x_kernel_v5: emit,
        seen_ids: HashSet::new(),
        last: None,
        game: None,
        episodes: 0,
        halts: 0,
    };
    let stdin = io::stdin();
    let mut stdout = io::stdout().lock();
    for line in stdin.lock().lines() {
        let line = match line {
            Ok(l) => l,
            Err(e) => {
                eprintln!("agent_bridge_v1: stdin read failed: {e}");
                std::process::exit(1);
            }
        };
        let line = line.strip_suffix('\r').unwrap_or(&line).to_string();
        let out = bridge.handle(&line);
        if writeln!(stdout, "{out}").and_then(|_| stdout.flush()).is_err() {
            std::process::exit(1);
        }
    }
    if bridge.halts > 0 {
        eprintln!("agent_bridge_v1: {} halted game(s)", bridge.halts);
    }
}
