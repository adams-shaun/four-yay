//! agent_bridge_v2 (gorge reconstruction): serves the SpellBench protocol v2
//! ENVIRONMENT role (`spec/SPELLBENCH_PROTOCOL_V2.md`, Sections 2-9) over
//! stdin/stdout NDJSON on top of mtg-kernel's public in-process
//! `RlEpisodeSessionV1` (policy surface V5, legal-answers-only scans).
//!
//! Jack Maiorino's own `agent_bridge_v2` (the engine the upstream
//! `benchmarks/pauper-kernel` definition launches with `--x-kernel-flat-v4`)
//! is not public. This is a clean-room rebuild from the public spec, its
//! informative Annex A and the arena's live validator. Local only; not
//! upstreamed.
//!
//! What it implements:
//! - strict NDJSON transport (I-JSON: duplicate keys, fractions, exponents,
//!   integers past 2^53-1, nesting past 64, unpaired surrogates, invalid
//!   UTF-8 and lines over 8 MiB are `malformed_json`), the closed error
//!   codes, the one-entry retransmission cache, `hello`/`reset`/`step`,
//!   `validate_deck`, and `probe_resample` answered `unsupported_request`;
//! - all game randomness seeded from `reset.game_secret` (the kernel's
//!   environment-v2 deck-pair builder: per-seat, per-purpose shuffle streams
//!   derived from one 64-bit root, itself HMAC-derived from the secret);
//! - per-viewer object ids per Section 5.3's recommended construction
//!   (HMAC of the game secret; fresh on every zone change and every look);
//! - the full Section 6 observation projected from `ObservationV5`, with
//!   the optional flags `designations`, `passed_seats`, `keywords`,
//!   `exiled_by`, `permanent_details` and `known_cards`;
//! - the kernel's action semantics mapped onto the v2.0 kinds, `pass` first,
//!   hidden-zone candidates in (card_name, object_id) order, the kernel's
//!   attacker/blocker inclusion scans as fixed-size `declare_attack` /
//!   `declare_block` groups, and the kernel's permutation-valued trigger
//!   ordering decomposed into a sequential `order_pick` group;
//! - `engine_defaults.mana_payment: engine_autopay`: the kernel's cast and
//!   activation payment taps untapped sources itself (its mana planner);
//!   mana abilities are still offered at priority, for floating mana;
//! - per-seat `seat_step`, per-seat group ids, the host's caps (`max_steps`,
//!   `max_decisions`) enforced by the bridge itself.
//!
//! What it does not: `--x-kernel-flat-v4` (the private flat encoder
//! contract) is refused; no `x_` extension is emitted (`x_kernel_v5` is a
//! native-id extension whose rated use is reserved); decklists as data,
//! the London mulligan and `toss_winner_chooses` are not offered.

use mtg_kernel::card_def::{
    mana_color_mask, CardDef, CardType, Keywords, Subtype, Supertype, CARD_DEFS,
    KERNEL_CARDDB_HASH,
};
use mtg_kernel::engine::{CastMode, CostKind, OptionalCostChoice};
use mtg_kernel::ids::PlayerId;
use mtg_kernel::mana::ManaColor;
use mtg_kernel::rl::{
    card_name, ActionSemanticV1, BooleanChoicePurposeV4, CardPublicV2, CardStableRefV1,
    EngineDecisionStageV2, ObjectRelationPublicV4, ObservationV5, PendingEffectChoiceSemanticV4,
    PlayerSeatV1, StackItemKindV2, TargetRefV1, TargetSelectionPurposeV4, TerminalOutcomeV1,
    ZoneIndependentStepV1,
};
use mtg_kernel::rl_session::{RlEpisodeSessionV1, RlSessionDecisionV1, RlSessionResponseV1};
use mtg_kernel::runtime_decks::{runtime_deck_by_id, RUNTIME_DECKS};
use mtg_kernel::state::Zone;
use mtg_kernel::KERNEL_VERSION;
use serde_json::{json, Map, Value};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet, HashMap, HashSet};
use std::io::{self, BufRead, Write};

const PROTOCOL: &str = "spellbench/v2";
const FORMAT: &str = "pauper-bo1";
const MAX_LINE: usize = 8 << 20;
const MAX_SAFE: u64 = (1 << 53) - 1;
const MAX_DEPTH: usize = 64;
const SEATS: [&str; 2] = ["p0", "p1"];
const BRIDGE_ID: &str = "gorge-agent-bridge-v2";

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

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

fn malformed(message: impl Into<String>) -> Err {
    err("malformed_request", message)
}

// ---------------------------------------------------------------------------
// Strict JSON (spec 2: I-JSON, integers only, depth <= 64)
// ---------------------------------------------------------------------------

struct Parser<'a> {
    s: &'a [u8],
    i: usize,
}

impl<'a> Parser<'a> {
    fn ws(&mut self) {
        while self.i < self.s.len() && matches!(self.s[self.i], b' ' | b'\t' | b'\n' | b'\r') {
            self.i += 1;
        }
    }

    fn peek(&self) -> Option<u8> {
        self.s.get(self.i).copied()
    }

    fn value(&mut self, depth: usize) -> Result<Value, String> {
        self.ws();
        match self.peek() {
            Some(b'{') => self.object(depth + 1),
            Some(b'[') => self.array(depth + 1),
            Some(b'"') => Ok(Value::String(self.string()?)),
            Some(b't') => self.literal("true", Value::Bool(true)),
            Some(b'f') => self.literal("false", Value::Bool(false)),
            Some(b'n') => self.literal("null", Value::Null),
            Some(b'-') | Some(b'0'..=b'9') => self.number(),
            Some(c) => Err(format!("unexpected byte 0x{c:02x} at {}", self.i)),
            None => Err("unexpected end of input".into()),
        }
    }

    fn literal(&mut self, word: &str, v: Value) -> Result<Value, String> {
        if self.s[self.i..].starts_with(word.as_bytes()) {
            self.i += word.len();
            Ok(v)
        } else {
            Err(format!("invalid literal at {}", self.i))
        }
    }

    fn object(&mut self, depth: usize) -> Result<Value, String> {
        if depth > MAX_DEPTH {
            return Err("nesting deeper than 64 levels".into());
        }
        self.i += 1;
        let mut m = Map::new();
        self.ws();
        if self.peek() == Some(b'}') {
            self.i += 1;
            return Ok(Value::Object(m));
        }
        loop {
            self.ws();
            if self.peek() != Some(b'"') {
                return Err(format!("expected a key at {}", self.i));
            }
            let k = self.string()?;
            self.ws();
            if self.peek() != Some(b':') {
                return Err(format!("expected ':' at {}", self.i));
            }
            self.i += 1;
            let v = self.value(depth)?;
            if m.contains_key(&k) {
                return Err(format!("duplicate object key {k:?}"));
            }
            m.insert(k, v);
            self.ws();
            match self.peek() {
                Some(b',') => self.i += 1,
                Some(b'}') => {
                    self.i += 1;
                    return Ok(Value::Object(m));
                }
                _ => return Err(format!("expected ',' or '}}' at {}", self.i)),
            }
        }
    }

    fn array(&mut self, depth: usize) -> Result<Value, String> {
        if depth > MAX_DEPTH {
            return Err("nesting deeper than 64 levels".into());
        }
        self.i += 1;
        let mut a = Vec::new();
        self.ws();
        if self.peek() == Some(b']') {
            self.i += 1;
            return Ok(Value::Array(a));
        }
        loop {
            a.push(self.value(depth)?);
            self.ws();
            match self.peek() {
                Some(b',') => self.i += 1,
                Some(b']') => {
                    self.i += 1;
                    return Ok(Value::Array(a));
                }
                _ => return Err(format!("expected ',' or ']' at {}", self.i)),
            }
        }
    }

    fn number(&mut self) -> Result<Value, String> {
        let neg = self.peek() == Some(b'-');
        if neg {
            self.i += 1;
        }
        let start = self.i;
        match self.peek() {
            Some(b'0') => self.i += 1,
            Some(b'1'..=b'9') => {
                while matches!(self.peek(), Some(b'0'..=b'9')) {
                    self.i += 1;
                }
            }
            _ => return Err(format!("invalid number at {}", self.i)),
        }
        if matches!(self.peek(), Some(b'.') | Some(b'e') | Some(b'E')) {
            return Err("numbers with a fraction or an exponent are not allowed".into());
        }
        let digits = &self.s[start..self.i];
        if digits.len() > 20 {
            return Err("integer out of range".into());
        }
        let mut n: u128 = 0;
        for d in digits {
            n = n * 10 + u128::from(d - b'0');
        }
        if n > u128::from(MAX_SAFE) {
            return Err("integer outside [-(2^53-1), 2^53-1]".into());
        }
        Ok(if neg {
            if n == 0 {
                Value::from(0u64)
            } else {
                Value::from(-(n as i64))
            }
        } else {
            Value::from(n as u64)
        })
    }

    fn hex4(&mut self) -> Result<u32, String> {
        if self.i + 4 > self.s.len() {
            return Err("truncated \\u escape".into());
        }
        let h = std::str::from_utf8(&self.s[self.i..self.i + 4]).map_err(|_| "bad \\u escape")?;
        let v = u32::from_str_radix(h, 16).map_err(|_| "bad \\u escape".to_string())?;
        self.i += 4;
        Ok(v)
    }

    fn string(&mut self) -> Result<String, String> {
        self.i += 1; // opening quote
        let mut out = String::new();
        loop {
            let start = self.i;
            while self.i < self.s.len() && self.s[self.i] != b'"' && self.s[self.i] != b'\\' {
                if self.s[self.i] < 0x20 {
                    return Err("raw control character in a string".into());
                }
                self.i += 1;
            }
            out.push_str(std::str::from_utf8(&self.s[start..self.i]).map_err(|_| "invalid UTF-8")?);
            match self.peek() {
                None => return Err("unterminated string".into()),
                Some(b'"') => {
                    self.i += 1;
                    return Ok(out);
                }
                _ => {
                    self.i += 1;
                    let e = self.peek().ok_or("unterminated escape")?;
                    self.i += 1;
                    match e {
                        b'"' => out.push('"'),
                        b'\\' => out.push('\\'),
                        b'/' => out.push('/'),
                        b'b' => out.push('\u{8}'),
                        b'f' => out.push('\u{c}'),
                        b'n' => out.push('\n'),
                        b'r' => out.push('\r'),
                        b't' => out.push('\t'),
                        b'u' => {
                            let hi = self.hex4()?;
                            if (0xD800..0xDC00).contains(&hi) {
                                if self.s[self.i..].starts_with(b"\\u") {
                                    self.i += 2;
                                    let lo = self.hex4()?;
                                    if !(0xDC00..0xE000).contains(&lo) {
                                        return Err("unpaired surrogate escape".into());
                                    }
                                    let c = 0x10000 + ((hi - 0xD800) << 10) + (lo - 0xDC00);
                                    out.push(char::from_u32(c).ok_or("bad surrogate pair")?);
                                } else {
                                    return Err("unpaired surrogate escape".into());
                                }
                            } else if (0xDC00..0xE000).contains(&hi) {
                                return Err("unpaired surrogate escape".into());
                            } else {
                                out.push(char::from_u32(hi).ok_or("bad \\u escape")?);
                            }
                        }
                        _ => return Err("invalid escape".into()),
                    }
                }
            }
        }
    }
}

fn parse_strict(line: &[u8]) -> Result<Value, String> {
    if std::str::from_utf8(line).is_err() {
        return Err("invalid UTF-8".into());
    }
    let mut p = Parser { s: line, i: 0 };
    let v = p.value(0)?;
    p.ws();
    if p.i != line.len() {
        return Err(format!("trailing data at {}", p.i));
    }
    Ok(v)
}

fn canonical(v: &Value) -> String {
    // serde_json's Map is a BTreeMap here (no preserve_order): keys sorted by
    // code point (all protocol keys are ASCII), compact separators.
    serde_json::to_string(v).expect("json serializes")
}

// ---------------------------------------------------------------------------
// HMAC-SHA256, ids, secrets (spec 5.3, 11.6)
// ---------------------------------------------------------------------------

fn hmac(key: &[u8], msg: &[u8]) -> [u8; 32] {
    let mut k = [0u8; 64];
    if key.len() > 64 {
        k[..32].copy_from_slice(&Sha256::digest(key));
    } else {
        k[..key.len()].copy_from_slice(key);
    }
    let mut ipad = [0x36u8; 64];
    let mut opad = [0x5cu8; 64];
    for i in 0..64 {
        ipad[i] ^= k[i];
        opad[i] ^= k[i];
    }
    let mut inner = Sha256::new();
    inner.update(ipad);
    inner.update(msg);
    let ih = inner.finalize();
    let mut outer = Sha256::new();
    outer.update(opad);
    outer.update(ih);
    outer.finalize().into()
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

fn unhex32(s: &str) -> Option<[u8; 32]> {
    if s.len() != 64 || !s.bytes().all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f')) {
        return None;
    }
    let mut out = [0u8; 32];
    for i in 0..32 {
        out[i] = u8::from_str_radix(&s[2 * i..2 * i + 2], 16).ok()?;
    }
    Some(out)
}

fn object_id(id_key: &[u8; 32], viewer: usize, key: &str) -> String {
    let mac = hmac(id_key, format!("{}:{}", SEATS[viewer], key).as_bytes());
    format!("o-{}", hex(&mac[..8]))
}

fn sha256_tagged(s: &str) -> String {
    format!("sha256:{}", hex(&Sha256::digest(s.as_bytes())))
}

fn self_test() -> bool {
    let run_secret: Vec<u8> = (0u8..32).collect();
    let gs0 = hmac(&run_secret, b"spellbench/v2/game:0");
    let mut ok = hex(&gs0) == "7648831b4ae4148770e13149d5ebbe1c4991168413d4b38e49292cfc5538980e";
    let id_key = hmac(&gs0, b"spellbench/v2/object-id");
    ok &= hex(&id_key) == "842e5229d41477f389ae25e2b8196afb5bfa8c6bd9b95d7bd3703031c88e22e6";
    ok &= object_id(&id_key, 0, "card-17:z2") == "o-0a3647243d16bf78";
    ok &= object_id(&id_key, 1, "card-17:z2") == "o-e5e4b7ed2a0730e4";
    ok &= object_id(&id_key, 0, "card-17:z2:look:0") == "o-794a5cb152c9620f";
    ok &= object_id(&id_key, 0, "card-17:z2:look:1") == "o-e18a35822cc60e1c";
    let rng = hmac(&gs0, b"spellbench/v2/rng:p1:library_shuffle:0");
    ok &= hex(&rng[..8]) == "8a28fd4db75719b1";
    let rows = json!([{"count": 4, "name": "Lightning Bolt"}, {"count": 18, "name": "Mountain"}]);
    ok &= sha256_tagged(&canonical(&rows))
        == "sha256:0df0a001e3c4b74b1061b21e319a645f32fbe3173120e432864e14d6d6f2f5d2";
    let t = parse_strict("{\"b\":\"Chainer's Edict\",\"a\":\"Lim-D\\u00fbl's Vault\",\"c\":\"tab\\there\"}".as_bytes()).unwrap();
    ok &= hex(&Sha256::digest(canonical(&t).as_bytes()))
        == "041575311eb1deb02f63f70361e14159034faf0d2a31e57edf8b4cf037680377";
    ok
}

// ---------------------------------------------------------------------------
// Vocabulary helpers
// ---------------------------------------------------------------------------

fn seat_of(p: PlayerSeatV1) -> usize {
    match p {
        PlayerSeatV1::P0 => 0,
        PlayerSeatV1::P1 => 1,
    }
}

fn zone_name(z: Zone) -> &'static str {
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

/// Kernel card names are mostly Oracle names already; these are the pool's
/// exceptions (ASCII-folded names, and multi-face cards whose decklist name
/// is the full "A // B").
fn oracle_face_name(raw: &str) -> String {
    match raw {
        "Lorien Revealed" => "Lórien Revealed".to_string(),
        "Troll of Khazad-dum" => "Troll of Khazad-dûm".to_string(),
        other => other.to_string(),
    }
}

fn oracle_deck_name(raw: &str) -> String {
    match raw {
        "Sagu Wildling" => "Sagu Wildling // Roost Seek".to_string(),
        "The Modern Age" => "The Modern Age // Vector Glider".to_string(),
        other => oracle_face_name(other),
    }
}

fn def_of(card_db_id: u16) -> &'static CardDef {
    &CARD_DEFS[card_db_id as usize]
}

fn name_of(card_db_id: u16) -> String {
    oracle_face_name(&card_name(card_db_id))
}

fn snake_word(debug: &str) -> String {
    let base = debug.strip_suffix("AllCaps").unwrap_or(debug);
    let mut out = String::new();
    for (i, ch) in base.chars().enumerate() {
        if ch.is_ascii_uppercase() {
            if i > 0 {
                out.push('_');
            }
            out.push(ch.to_ascii_lowercase());
        } else if ch.is_ascii_alphanumeric() {
            out.push(ch);
        }
    }
    out
}

fn subtype_name(s: Subtype) -> String {
    snake_word(&format!("{s:?}"))
}

fn color_words_from_mask(mask: u8) -> Vec<&'static str> {
    let mut out = Vec::new();
    for (c, w) in [
        (ManaColor::W, "white"),
        (ManaColor::U, "blue"),
        (ManaColor::B, "black"),
        (ManaColor::R, "red"),
        (ManaColor::G, "green"),
    ] {
        if mask & mana_color_mask(c) != 0 {
            out.push(w);
        }
    }
    out
}

fn color_word(c: ManaColor) -> Option<&'static str> {
    match c {
        ManaColor::W => Some("white"),
        ManaColor::U => Some("blue"),
        ManaColor::B => Some("black"),
        ManaColor::R => Some("red"),
        ManaColor::G => Some("green"),
        ManaColor::C => None,
    }
}

fn mana_symbol(c: ManaColor) -> &'static str {
    match c {
        ManaColor::W => "W",
        ManaColor::U => "U",
        ManaColor::B => "B",
        ManaColor::R => "R",
        ManaColor::G => "G",
        ManaColor::C => "C",
    }
}

fn type_word(t: CardType) -> &'static str {
    match t {
        CardType::Land => "land",
        CardType::Creature => "creature",
        CardType::Instant => "instant",
        CardType::Sorcery => "sorcery",
        CardType::Artifact => "artifact",
        CardType::Enchantment => "enchantment",
        CardType::Planeswalker => "planeswalker",
    }
}

fn supertype_words(def: &CardDef) -> Vec<&'static str> {
    def.supertypes
        .iter()
        .map(|s| match s {
            Supertype::Basic => "basic",
            Supertype::Snow => "snow",
            Supertype::Legendary => "legendary",
        })
        .collect()
}

fn def_keywords(k: Keywords) -> Vec<&'static str> {
    let table = [
        (Keywords::FLYING, "flying"),
        (Keywords::REACH, "reach"),
        (Keywords::HASTE, "haste"),
        (Keywords::VIGILANCE, "vigilance"),
        (Keywords::TRAMPLE, "trample"),
        (Keywords::FIRST_STRIKE, "first_strike"),
        (Keywords::DOUBLE_STRIKE, "double_strike"),
        (Keywords::DEATHTOUCH, "deathtouch"),
        (Keywords::MENACE, "menace"),
        (Keywords::DEFENDER, "defender"),
        (Keywords::LIFELINK, "lifelink"),
        (Keywords::HEXPROOF, "hexproof"),
        (Keywords::INDESTRUCTIBLE, "indestructible"),
        (Keywords::PROTECTION_FROM_MONOCOLORED, "protection"),
        (Keywords::ISLANDWALK, "landwalk"),
        (Keywords::FLASH, "flash"),
    ];
    table
        .iter()
        .filter(|(bit, _)| k.has(*bit))
        .map(|(_, w)| *w)
        .collect()
}

fn dedup_strings(v: Vec<String>) -> Vec<String> {
    let mut seen = HashSet::new();
    v.into_iter().filter(|s| !s.is_empty() && seen.insert(s.clone())).collect()
}

/// Printed characteristics from the card definition (hand, stack, looked-at
/// library cards).
fn def_characteristics(card_db_id: u16) -> Value {
    let def = def_of(card_db_id);
    let creature = def.types.contains(&CardType::Creature);
    let mut kws: Vec<&str> = def_keywords(def.keywords);
    if def.ward_cost.is_some() && !kws.contains(&"ward") {
        kws.push("ward");
    }
    json!({
        "supertypes": supertype_words(def),
        "types": def.types.iter().map(|t| type_word(*t)).collect::<Vec<_>>(),
        "subtypes": dedup_strings(def.subtypes.iter().map(|s| subtype_name(*s)).collect()),
        "colors": color_words_from_mask(mtg_kernel::card_def::mana_colors_mask(def.colors)),
        "mana_value": def.mana_value,
        "power": if creature { def.power.map(i32::from) } else { None },
        "toughness": if creature { def.toughness.map(i32::from) } else { None },
        "keywords": kws,
    })
}

// ---------------------------------------------------------------------------
// Engine identity
// ---------------------------------------------------------------------------

fn engine_identity() -> Value {
    json!({
        "name": "mtg-kernel",
        "version": KERNEL_VERSION,
        "source_revision": option_env!("SB_BRIDGE_SOURCE_REVISION"),
        "rules_snapshot_id": format!("mtg-kernel-rules/{}/carddb-{:016x}/{}", KERNEL_VERSION, KERNEL_CARDDB_HASH, BRIDGE_ID),
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

const DECISION_KINDS: [&str; 23] = [
    "pass",
    "play_land",
    "cast_spell",
    "activate_mana_ability",
    "activate_ability",
    "special_action",
    "choose_target",
    "finish_target_selection",
    "choose_cost_target",
    "choose_cast_method",
    "choose_spell_mode",
    "choose_option",
    "choose_color",
    "choose_number",
    "choose_boolean",
    "select_object",
    "finish_selection",
    "optional_cost",
    "choose_cost_option",
    "optional_cast",
    "order_pick",
    "declare_attack",
    "declare_block",
];

const PRIORITY_KINDS: [&str; 6] = [
    "pass",
    "play_land",
    "cast_spell",
    "activate_mana_ability",
    "activate_ability",
    "special_action",
];

fn observation_flags() -> Value {
    json!({
        "poison": false, "player_counters": false, "designations": true, "player_progress": false,
        "day_night": false, "passed_seats": true, "pending_triggers": false, "keywords": true,
        "full_name": false, "exiled_by": true, "stack_text": false, "permanent_details": true,
        "known_cards": true,
    })
}

/// Catalog decklist rows `{name, count}`, sorted by name.
fn catalog_rows(id: &str) -> Option<Vec<(String, u32)>> {
    let deck = runtime_deck_by_id(id)?;
    let mut counts: BTreeMap<String, u32> = BTreeMap::new();
    for &c in deck.card_ids {
        *counts.entry(oracle_deck_name(&card_name(c))).or_default() += 1;
    }
    Some(counts.into_iter().collect())
}

fn deck_id_of(rows: &[(String, u32)]) -> String {
    let mut sorted: Vec<&(String, u32)> = rows.iter().collect();
    sorted.sort_by(|a, b| a.0.cmp(&b.0));
    let v: Vec<Value> = sorted.iter().map(|(n, c)| json!({"count": c, "name": n})).collect();
    sha256_tagged(&canonical(&Value::Array(v)))
}

fn hello_ok(request_id: &str) -> Value {
    let catalog: Vec<Value> = RUNTIME_DECKS
        .iter()
        .filter(|d| d.is_fully_materialized())
        .map(|d| {
            let rows = catalog_rows(d.id).expect("catalog deck");
            json!({
                "catalog_id": d.id,
                "name": d.id,
                "decklist": rows.iter().map(|(n, c)| json!({"name": n, "count": c})).collect::<Vec<_>>(),
            })
        })
        .collect();
    let mut m = envelope("hello_ok", request_id);
    m.insert("protocol_minor".into(), json!(0));
    m.insert("engine".into(), engine_identity());
    m.insert("formats".into(), json!([FORMAT]));
    m.insert("deck_sources".into(), json!(["catalog"]));
    m.insert("catalog".into(), Value::Array(catalog));
    m.insert(
        "rules_supported".into(),
        json!({"mulligan": ["none"], "starting_player": ["host_assigned"]}),
    );
    m.insert("observation".into(), observation_flags());
    m.insert("decision_kinds".into(), json!(DECISION_KINDS));
    m.insert(
        "engine_defaults".into(),
        json!({"trigger_order": null, "replacement_order": null,
               "combat_damage_assignment": "engine_order", "mana_payment": "engine_autopay"}),
    );
    m.insert("rewind".into(), json!(false));
    m.insert("fairness".into(), json!({"noninterference_probe": false}));
    m.insert("extensions".into(), json!([]));
    Value::Object(m)
}

fn envelope(kind: &str, request_id: &str) -> Map<String, Value> {
    let mut m = Map::new();
    m.insert("response_type".into(), json!(kind));
    m.insert("protocol".into(), json!(PROTOCOL));
    m.insert("request_id".into(), json!(request_id));
    m
}

fn error_response(request_id: &str, e: &Err) -> Value {
    let mut msg: String = e.message.chars().filter(|c| !c.is_control()).take(240).collect();
    if msg.is_empty() {
        msg = e.code.to_string();
    }
    let mut m = envelope("error", request_id);
    m.insert("error".into(), json!({"code": e.code, "message": msg}));
    Value::Object(m)
}

// ---------------------------------------------------------------------------
// Strict request field checks
// ---------------------------------------------------------------------------

fn obj<'a>(v: &'a Value, what: &str) -> Result<&'a Map<String, Value>, Err> {
    v.as_object().ok_or_else(|| malformed(format!("{what} must be an object")))
}

fn exact_keys(m: &Map<String, Value>, keys: &[&str], what: &str) -> Result<(), Err> {
    for k in m.keys() {
        if !keys.contains(&k.as_str()) {
            return Err(malformed(format!("{what}: unknown field {k:?}")));
        }
    }
    for k in keys {
        if !m.contains_key(*k) {
            return Err(malformed(format!("{what}: missing field {k:?}")));
        }
    }
    Ok(())
}

fn nonempty_str<'a>(m: &'a Map<String, Value>, k: &str) -> Result<&'a str, Err> {
    match m.get(k).and_then(Value::as_str) {
        Some(s) if !s.is_empty() => Ok(s),
        _ => Err(malformed(format!("{k} must be a nonempty string"))),
    }
}

fn safe_int(m: &Map<String, Value>, k: &str) -> Result<u64, Err> {
    match m.get(k).and_then(Value::as_u64) {
        Some(v) if v <= MAX_SAFE => Ok(v),
        _ => Err(malformed(format!("{k} must be an integer in [0, 2^53-1]"))),
    }
}

fn u32_field(m: &Map<String, Value>, k: &str) -> Result<u32, Err> {
    match m.get(k).and_then(Value::as_u64) {
        Some(v) if v <= u64::from(u32::MAX) => Ok(v as u32),
        _ => Err(malformed(format!("{k} must be a u32"))),
    }
}

fn check_decklist(v: &Value, what: &str) -> Result<(), Err> {
    let rows = v.as_array().ok_or_else(|| malformed(format!("{what} must be an array")))?;
    if rows.is_empty() {
        return Err(malformed(format!("{what} must be nonempty")));
    }
    let mut names = HashSet::new();
    for r in rows {
        let r = obj(r, what)?;
        exact_keys(r, &["name", "count"], what)?;
        let n = nonempty_str(r, "name")?;
        let c = u32_field(r, "count")?;
        if c < 1 {
            return Err(malformed(format!("{what}: count must be at least 1")));
        }
        if !names.insert(n.to_string()) {
            return Err(malformed(format!("{what}: duplicate name {n:?}")));
        }
    }
    Ok(())
}

fn is_deck_id(s: &str) -> bool {
    s.len() == 71
        && s.starts_with("sha256:")
        && s[7..].bytes().all(|c| matches!(c, b'0'..=b'9' | b'a'..=b'f'))
}

fn is_extension_name(s: &str) -> bool {
    s.len() > 2
        && s.starts_with("x_")
        && s[2..].bytes().all(|c| matches!(c, b'a'..=b'z' | b'0'..=b'9' | b'_'))
}

struct ResetReq {
    game_id: String,
    format: String,
    decks: [(String, Option<String>, bool); 2], // (deck_id, catalog_id, is_decklist)
    rules: Map<String, Value>,
    secret: [u8; 32],
    max_decisions: u64,
    max_steps: u64,
}

fn parse_rules(v: &Value) -> Result<Map<String, Value>, Err> {
    let r = obj(v, "rules")?;
    exact_keys(
        r,
        &["opponent_decklist", "mulligan", "starting_player", "starting_seat", "card_name_domain", "extensions", "probe"],
        "rules",
    )?;
    let od = r["opponent_decklist"].as_str();
    if !matches!(od, Some("visible") | Some("hidden")) {
        return Err(malformed("rules.opponent_decklist must be visible or hidden"));
    }
    if !matches!(r["mulligan"].as_str(), Some("london") | Some("none")) {
        return Err(malformed("rules.mulligan must be london or none"));
    }
    match r["starting_player"].as_str() {
        Some("host_assigned") => {
            if !matches!(r["starting_seat"].as_str(), Some("p0") | Some("p1")) {
                return Err(malformed("rules.starting_seat must be p0 or p1 with host_assigned"));
            }
        }
        Some("toss_winner_chooses") => {
            if !r["starting_seat"].is_null() {
                return Err(malformed("rules.starting_seat must be null with toss_winner_chooses"));
            }
        }
        _ => return Err(malformed("rules.starting_player must be host_assigned or toss_winner_chooses")),
    }
    let dom = obj(&r["card_name_domain"], "rules.card_name_domain")?;
    exact_keys(dom, &["domain_id", "names"], "rules.card_name_domain")?;
    let names = dom["names"]
        .as_array()
        .ok_or_else(|| malformed("rules.card_name_domain.names must be an array"))?;
    let mut seen = HashSet::new();
    let mut sorted: Vec<String> = Vec::new();
    for n in names {
        let s = n
            .as_str()
            .filter(|s| !s.is_empty())
            .ok_or_else(|| malformed("rules.card_name_domain.names holds nonempty strings"))?;
        if !seen.insert(s.to_string()) {
            return Err(malformed("rules.card_name_domain.names must be distinct"));
        }
        sorted.push(s.to_string());
    }
    sorted.sort();
    let expected = sha256_tagged(&canonical(&json!(sorted)));
    if dom["domain_id"].as_str() != Some(expected.as_str()) {
        return Err(malformed("rules.card_name_domain.domain_id does not match its names"));
    }
    let exts = r["extensions"]
        .as_array()
        .ok_or_else(|| malformed("rules.extensions must be an array"))?;
    let mut seen = HashSet::new();
    for e in exts {
        let s = e.as_str().filter(|s| is_extension_name(s)).ok_or_else(|| malformed("rules.extensions holds x_ names"))?;
        if !seen.insert(s) {
            return Err(malformed("rules.extensions must be distinct"));
        }
    }
    if !r["probe"].is_boolean() {
        return Err(malformed("rules.probe must be a boolean"));
    }
    Ok(r.clone())
}

fn parse_reset(m: &Map<String, Value>) -> Result<ResetReq, Err> {
    exact_keys(
        m,
        &["request_type", "protocol", "request_id", "game_id", "format", "seats", "rules", "game_secret", "max_decisions", "max_steps"],
        "reset",
    )?;
    let game_id = nonempty_str(m, "game_id")?.to_string();
    let format = nonempty_str(m, "format")?.to_string();
    let seats = m["seats"]
        .as_array()
        .filter(|a| a.len() == 2)
        .ok_or_else(|| malformed("seats must hold the p0 deck and the p1 deck"))?;
    let mut decks: [(String, Option<String>, bool); 2] = Default::default();
    for (i, s) in seats.iter().enumerate() {
        let s = obj(s, "seats entry")?;
        exact_keys(s, &["seat", "deck"], "seats entry")?;
        if s["seat"].as_str() != Some(SEATS[i]) {
            return Err(malformed("seats must list p0 then p1"));
        }
        let d = obj(&s["deck"], "deck")?;
        let keys: BTreeSet<&str> = d.keys().map(String::as_str).collect();
        let deck_id = d
            .get("deck_id")
            .and_then(Value::as_str)
            .filter(|s| is_deck_id(s))
            .ok_or_else(|| malformed("deck.deck_id must be sha256: and 64 lowercase hex"))?
            .to_string();
        if keys == BTreeSet::from(["deck_id", "catalog_id"]) {
            let c = nonempty_str(d, "catalog_id")?.to_string();
            decks[i] = (deck_id, Some(c), false);
        } else if keys == BTreeSet::from(["deck_id", "decklist"]) {
            check_decklist(&d["decklist"], "deck.decklist")?;
            decks[i] = (deck_id, None, true);
        } else {
            return Err(malformed("deck must be exactly {deck_id, catalog_id} or {deck_id, decklist}"));
        }
    }
    let rules = parse_rules(&m["rules"])?;
    let secret = m
        .get("game_secret")
        .and_then(Value::as_str)
        .and_then(unhex32)
        .ok_or_else(|| malformed("game_secret must be 64 lowercase hex digits"))?;
    let max_decisions = safe_int(m, "max_decisions")?;
    let max_steps = safe_int(m, "max_steps")?;
    Ok(ResetReq {
        game_id,
        format,
        decks,
        rules,
        secret,
        max_decisions,
        max_steps,
    })
}

// ---------------------------------------------------------------------------
// Game state the bridge keeps beside the kernel session
// ---------------------------------------------------------------------------

#[derive(Clone)]
enum Answer {
    Kernel(usize),
    OrderItem(usize),
}

struct Pending {
    seat: usize,
    semantics: Vec<Value>,
    answers: Vec<Answer>,
    group: (u64, u32, u32),
    kernel_step: u64,
    seq_tag: Option<String>,
    kinds: Vec<String>,
}

struct OrderState {
    seat: usize,
    n: usize,
    perms: Vec<Vec<usize>>, // kernel candidate i's order
    picked: Vec<usize>,
    group_id: u64,
}

#[derive(Default)]
struct LookState {
    key: String,
    epoch: u64,
    ided: BTreeSet<String>,
}

struct Game {
    game_id: String,
    episode: u64,
    session: RlEpisodeSessionV1,
    id_key: [u8; 32],
    max_steps: u64,
    max_decisions: u64,
    step: u64,
    seat_steps: [u64; 2],
    next_group: [u64; 2],
    partial: [Option<(u64, u32, u32)>; 2],
    completed: u64,
    pending: Option<Pending>,
    over: bool,
    abilities: Vec<(u32, String, String, u64)>,
    next_serial: u64,
    look: [LookState; 2],
    seq: [Option<(String, u32)>; 2],
    order: Option<OrderState>,
    subtype_names: HashMap<u16, String>,
}

/// One seat's view of the current kernel decision: records by internal key.
struct View {
    viewer: usize,
    records: HashMap<String, Value>,
    by_arena: HashMap<u32, String>,
    hidden: BTreeMap<String, (CardStableRefV1, Value)>, // referenced hidden-zone cards
    look_epoch: u64,
    look_ided: BTreeSet<String>,
    fallbacks: Vec<String>,
}

fn ckey(r: &CardStableRefV1) -> String {
    format!("c{}:{}:{}", r.arena_id, zone_name(r.zone), r.zone_change_count)
}

fn is_hidden_zone(r: &CardStableRefV1, viewer: usize) -> bool {
    match r.zone {
        Zone::Library => true,
        Zone::Hand => seat_of(r.owner) != viewer,
        _ => false,
    }
}

impl View {
    fn reference(&mut self, r: &CardStableRefV1, id_key: &[u8; 32]) -> Option<Value> {
        let key = ckey(r);
        if let Some(v) = self.records.get(&key) {
            return Some(v.clone());
        }
        if is_hidden_zone(r, self.viewer) {
            if let Some((_, v)) = self.hidden.get(&key) {
                return Some(v.clone());
            }
            let id = object_id(id_key, self.viewer, &format!("{key}:look:{}", self.look_epoch));
            let owner = SEATS[seat_of(r.owner)];
            let v = json!({
                "object_id": id,
                "card_name": name_of(r.card_db_id),
                "owner_seat": owner,
                "controller_seat": owner,
                "zone": zone_name(r.zone),
            });
            self.hidden.insert(key, (r.clone(), v.clone()));
            return Some(v);
        }
        if let Some(k) = self.by_arena.get(&r.arena_id) {
            self.fallbacks.push(format!("{key}->{k}"));
            return self.records.get(k).cloned();
        }
        None
    }

    fn target(&mut self, t: &TargetRefV1, id_key: &[u8; 32]) -> Option<Value> {
        match t {
            TargetRefV1::Player { player } => Some(json!({"player": SEATS[seat_of(*player)]})),
            TargetRefV1::Object { object } => self.reference(object, id_key).map(|r| json!({"object": r})),
        }
    }
}

fn reference_of(record: &Value) -> Value {
    json!({
        "object_id": record["object_id"],
        "card_name": record["card_name"],
        "owner_seat": record["owner_seat"],
        "controller_seat": record["controller_seat"],
        "zone": record["zone"],
    })
}

impl Game {
    fn halted(&mut self, request_id: &str, why: &str) -> Value {
        eprintln!("agent_bridge_v2: HALT game {} step {}: {why}", self.game_id, self.step);
        self.end(request_id, "halted", "halted", None, &format!("engine_contract_failure:{}", short_reason(why)))
    }

    fn end(&mut self, request_id: &str, outcome: &str, classification: &str, winner: Option<&str>, reason: &str) -> Value {
        self.over = true;
        self.pending = None;
        self.order = None;
        let mut m = envelope("terminal", request_id);
        m.insert("game_id".into(), json!(self.game_id));
        m.insert("outcome".into(), json!(outcome));
        m.insert("classification".into(), json!(classification));
        m.insert("winner".into(), json!(winner));
        m.insert("reason".into(), json!(reason));
        m.insert("step_count".into(), json!(self.step));
        m.insert("decision_count".into(), json!(self.completed));
        m.insert("provenance".into(), provenance());
        Value::Object(m)
    }

    fn look_key(obs: &ObservationV5) -> String {
        let ec = &obs.projection.surface.engine_context;
        let src = |r: &Option<CardStableRefV1>| r.as_ref().map(ckey).unwrap_or_default();
        let detail = match ec.current_stage {
            EngineDecisionStageV2::PendingEffect => ec.pending_effect.as_ref().map(|p| src(&p.source)).unwrap_or_default(),
            EngineDecisionStageV2::PendingCast => ec.pending_cast.as_ref().map(|p| src(&p.source)).unwrap_or_default(),
            EngineDecisionStageV2::PendingActivation => ec.pending_activation.as_ref().map(|p| src(&p.source)).unwrap_or_default(),
            _ => String::new(),
        };
        format!("{:?}|{detail}", ec.current_stage)
    }

    /// Builds the seat's observation (spec 6) and the reference table the
    /// candidates resolve against. `known` is finished later by `finish_known`.
    fn observe(&mut self, obs: &ObservationV5, viewer: usize, priority: bool) -> (Map<String, Value>, View) {
        let p = &obs.projection.surface;
        // Look epochs (spec 5.3: fresh ids on each look, stable within one effect).
        let lk = Self::look_key(obs);
        let look = &mut self.look[viewer];
        if look.key != lk {
            look.key = lk;
            look.epoch += 1;
            look.ided.clear();
        }
        let mut view = View {
            viewer,
            records: HashMap::new(),
            by_arena: HashMap::new(),
            hidden: BTreeMap::new(),
            look_epoch: look.epoch,
            look_ided: look.ided.clone(),
            fallbacks: Vec::new(),
        };
        let id_key = self.id_key;
        let mk_ref = |r: &CardStableRefV1, controller: usize| -> Value {
            json!({
                "object_id": object_id(&id_key, viewer, &ckey(r)),
                "card_name": name_of(r.card_db_id),
                "owner_seat": SEATS[seat_of(r.owner)],
                "controller_seat": SEATS[controller],
                "zone": zone_name(r.zone),
            })
        };
        // Pass 1: object records for every public zone and the own hand.
        let planeswalker_loyalty: HashMap<u32, u32> = p
            .engine_context
            .planeswalkers
            .as_ref()
            .map(|v| v.iter().map(|w| (w.permanent.arena_id, w.loyalty)).collect())
            .unwrap_or_default();
        let mut battlefield: [Vec<Value>; 2] = Default::default();
        let mut graveyard: [Vec<Value>; 2] = Default::default();
        let mut exile: [Vec<Value>; 2] = Default::default();
        let public_record = |c: &CardPublicV2, controller: usize, subtype_names: &HashMap<u16, String>| -> Value {
            let mut rec = mk_ref(&c.stable, controller);
            let ch = &c.characteristics;
            let def = def_of(c.stable.card_db_id);
            let mut types: Vec<&str> = Vec::new();
            let f = &ch.type_flags;
            for (on, w) in [
                (f.artifact, "artifact"),
                (f.creature, "creature"),
                (f.enchantment, "enchantment"),
                (f.instant, "instant"),
                (f.land, "land"),
            ] {
                if on {
                    types.push(w);
                }
            }
            if def.types.contains(&CardType::Planeswalker) {
                types.push("planeswalker");
            }
            if f.sorcery {
                types.push("sorcery");
            }
            let k = &ch.effective_keywords;
            let mut kws: Vec<&str> = Vec::new();
            for (on, w) in [
                (k.flying, "flying"),
                (k.reach, "reach"),
                (k.haste, "haste"),
                (k.vigilance, "vigilance"),
                (k.trample, "trample"),
                (k.first_strike, "first_strike"),
                (k.double_strike, "double_strike"),
                (k.deathtouch, "deathtouch"),
                (k.menace, "menace"),
                (k.defender, "defender"),
                (k.lifelink, "lifelink"),
                (k.hexproof, "hexproof"),
                (k.indestructible, "indestructible"),
                (k.protection_from_monocolored, "protection"),
                (k.ward_generic > 0, "ward"),
                (k.landwalk_mask != 0, "landwalk"),
            ] {
                if on {
                    kws.push(w);
                }
            }
            let subtypes = dedup_strings(
                ch.effective_subtype_ids
                    .iter()
                    .filter_map(|id| subtype_names.get(id).cloned())
                    .collect(),
            );
            let r = rec.as_object_mut().unwrap();
            r.insert("full_name".into(), Value::Null);
            r.insert("face_down".into(), json!(false));
            r.insert("token".into(), json!(c.is_token));
            r.insert("copy".into(), json!(false));
            r.insert(
                "characteristics".into(),
                json!({
                    "supertypes": supertype_words(def),
                    "types": types,
                    "subtypes": subtypes,
                    "colors": color_words_from_mask(ch.effective_color_mask),
                    "mana_value": def.mana_value,
                    "power": if f.creature { ch.effective_power } else { None },
                    "toughness": if f.creature { ch.effective_toughness } else { None },
                    "keywords": kws,
                }),
            );
            r.insert("permanent".into(), Value::Null);
            r.insert("exiled_by".into(), Value::Null);
            rec
        };
        for side in &p.battlefield {
            for c in side {
                let controller = seat_of(c.stable.controller);
                let mut rec = public_record(c, controller, &self.subtype_names);
                let mut counters = Map::new();
                for (n, v) in [
                    ("p1p1", c.counters.plus1_plus1),
                    ("m1m1", c.counters.minus1_minus1),
                    ("m0m1", c.counters.minus0_minus1),
                    ("stun", c.counters.stun),
                    ("lore", c.counters.lore),
                ] {
                    if v > 0 {
                        counters.insert(n.into(), json!(v as u32));
                    }
                }
                if let Some(l) = planeswalker_loyalty.get(&c.stable.arena_id) {
                    counters.insert("loyalty".into(), json!(*l));
                }
                let mut chosen = Vec::new();
                if let Some(color) = c.chosen_color.and_then(color_word) {
                    chosen.push(json!({"kind": "color", "value": color}));
                }
                rec["permanent"] = json!({
                    "tapped": c.tapped,
                    "summoning_sick": c.summoning_sick,
                    "damage": c.damage as u32,
                    "counters": counters,
                    "attached_to": null,
                    "attacking": false,
                    "attack_target": null,
                    "blocking": false,
                    "blocked_attackers": [],
                    "phased_out": false,
                    "statuses": if c.goaded_by.is_empty() { json!([]) } else { json!(["goaded"]) },
                    "class_level": null,
                    "chosen": chosen,
                });
                battlefield[controller].push(rec);
            }
        }
        for (owner, side) in p.graveyards.iter().enumerate() {
            for c in side {
                let o = seat_of(c.stable.owner);
                let _ = owner;
                graveyard[o].push(public_record(c, o, &self.subtype_names));
            }
        }
        for c in &p.exile {
            let o = seat_of(c.stable.owner);
            exile[o].push(public_record(c, o, &self.subtype_names));
        }
        let mut hand: Vec<Value> = Vec::new();
        for c in &obs.own_hand {
            let mut rec = mk_ref(&c.stable, viewer);
            let r = rec.as_object_mut().unwrap();
            r.insert("full_name".into(), Value::Null);
            r.insert("face_down".into(), json!(false));
            r.insert("token".into(), json!(def_of(c.stable.card_db_id).is_token));
            r.insert("copy".into(), json!(false));
            r.insert("characteristics".into(), def_characteristics(c.stable.card_db_id));
            r.insert("permanent".into(), Value::Null);
            r.insert("exiled_by".into(), Value::Null);
            hand.push(rec);
        }
        // Register every record's reference.
        let mut register = |view: &mut View, rec: &Value, key: String, arena: u32| {
            view.records.insert(key.clone(), reference_of(rec));
            view.by_arena.insert(arena, key);
        };
        for side in &p.battlefield {
            for c in side {
                let _ = c;
            }
        }
        // keys in the same order as the records were built
        {
            let mut i = [0usize; 2];
            for side in &p.battlefield {
                for c in side {
                    let ctl = seat_of(c.stable.controller);
                    let rec = battlefield[ctl][i[ctl]].clone();
                    i[ctl] += 1;
                    register(&mut view, &rec, ckey(&c.stable), c.stable.arena_id);
                }
            }
            let mut g = [0usize; 2];
            for side in &p.graveyards {
                for c in side {
                    let o = seat_of(c.stable.owner);
                    let rec = graveyard[o][g[o]].clone();
                    g[o] += 1;
                    register(&mut view, &rec, ckey(&c.stable), c.stable.arena_id);
                }
            }
            let mut e = [0usize; 2];
            for c in &p.exile {
                let o = seat_of(c.stable.owner);
                let rec = exile[o][e[o]].clone();
                e[o] += 1;
                register(&mut view, &rec, ckey(&c.stable), c.stable.arena_id);
            }
            for (c, rec) in obs.own_hand.iter().zip(&hand) {
                register(&mut view, rec, ckey(&c.stable), c.stable.arena_id);
            }
        }
        // Stack: spells keep their card's zone key; abilities get a serial
        // that lives as long as the entry stays on the stack.
        let mut items: Vec<_> = p.stack.iter().collect();
        items.sort_by_key(|s| s.stack_index);
        let mut live: Vec<(u32, String, String, u64)> = Vec::new();
        let mut stack_keys: Vec<String> = Vec::new();
        for s in &items {
            let kind = match s.stack_item_kind {
                StackItemKindV2::Spell => "spell",
                StackItemKindV2::ActivatedAbility => "activated_ability",
                _ => "triggered_ability",
            };
            if kind == "spell" {
                stack_keys.push(ckey(&s.source));
                continue;
            }
            let sk = ckey(&s.source);
            let serial = self
                .abilities
                .iter()
                .find(|(i, k, kd, _)| *i == s.stack_index && *k == sk && kd == kind)
                .map(|e| e.3)
                .unwrap_or_else(|| {
                    self.next_serial += 1;
                    self.next_serial
                });
            live.push((s.stack_index, sk, kind.to_string(), serial));
            stack_keys.push(format!("a{serial}"));
        }
        self.abilities = live;
        let mut stack_refs: Vec<Value> = Vec::new();
        for (s, key) in items.iter().zip(&stack_keys) {
            let controller = seat_of(s.controller);
            let spell = matches!(s.stack_item_kind, StackItemKindV2::Spell);
            let rec = json!({
                "object_id": object_id(&id_key, viewer, key),
                "card_name": name_of(s.source.card_db_id),
                "owner_seat": if spell { SEATS[seat_of(s.source.owner)] } else { SEATS[controller] },
                "controller_seat": SEATS[controller],
                "zone": "stack",
            });
            view.records.insert(key.clone(), rec.clone());
            if spell {
                view.by_arena.insert(s.source.arena_id, key.clone());
            }
            stack_refs.push(rec);
        }
        let mut stack: Vec<Value> = Vec::new();
        for ((s, key), rref) in items.iter().zip(&stack_keys).zip(&stack_refs) {
            let spell = matches!(s.stack_item_kind, StackItemKindV2::Spell);
            let def = def_of(s.source.card_db_id);
            let source = if spell {
                Value::Null
            } else {
                view.records.get(&ckey(&s.source)).cloned().unwrap_or(Value::Null)
            };
            let targets: Vec<Value> = s
                .targets
                .iter()
                .map(|t| match t {
                    TargetRefV1::Player { player } => json!({"player": SEATS[seat_of(*player)]}),
                    TargetRefV1::Object { object } => view
                        .records
                        .get(&ckey(object))
                        .map(|r| json!({"object": r}))
                        .unwrap_or(Value::Null),
                })
                .collect();
            let mut e = rref.as_object().unwrap().clone();
            let _ = key;
            e.insert(
                "stack_kind".into(),
                json!(match s.stack_item_kind {
                    StackItemKindV2::Spell => "spell",
                    StackItemKindV2::ActivatedAbility => "activated_ability",
                    _ => "triggered_ability",
                }),
            );
            e.insert("source".into(), source);
            e.insert("face_down".into(), json!(false));
            e.insert("copy".into(), json!(s.is_copy));
            e.insert(
                "characteristics".into(),
                if spell { def_characteristics(s.source.card_db_id) } else { Value::Null },
            );
            e.insert("targets".into(), Value::Array(targets));
            e.insert("divided".into(), Value::Null);
            e.insert(
                "modes".into(),
                if spell && def.mode2.is_some() { json!([s.mode_chosen as u32]) } else { Value::Null },
            );
            e.insert(
                "x_value".into(),
                if spell && def.cost.x_count > 0 { json!(s.x_value as u32) } else { Value::Null },
            );
            e.insert("text".into(), Value::Null);
            stack.push(Value::Object(e));
        }
        // Pass 2: relations and combat.
        let mut attached: HashMap<String, Value> = HashMap::new();
        let mut exiled_by: HashMap<String, Value> = HashMap::new();
        for rel in &p.object_relations {
            match rel {
                ObjectRelationPublicV4::AttachedTo { object, attached_to } => {
                    if let Some(r) = view.records.get(&ckey(attached_to)) {
                        attached.insert(ckey(object), json!({"object": r}));
                    }
                }
                ObjectRelationPublicV4::ExiledBy { object, exiled_by: by } => {
                    if let Some(r) = view.records.get(&ckey(by)) {
                        exiled_by.insert(ckey(object), r.clone());
                    }
                }
            }
        }
        let combat = &p.combat;
        let attackers: HashSet<String> = combat.ordered_attackers.iter().map(ckey).collect();
        let mut blocks: HashMap<String, Vec<Value>> = HashMap::new();
        for (a, bs) in &combat.attacker_to_ordered_blockers {
            if let Some(ar) = view.records.get(&ckey(a)) {
                for b in bs {
                    blocks.entry(ckey(b)).or_default().push(ar.clone());
                }
            } else {
                for b in bs {
                    blocks.entry(ckey(b)).or_default();
                }
            }
        }
        {
            let mut i = [0usize; 2];
            for side in &p.battlefield {
                for c in side {
                    let ctl = seat_of(c.stable.controller);
                    let k = ckey(&c.stable);
                    let rec = &mut battlefield[ctl][i[ctl]];
                    i[ctl] += 1;
                    let perm = rec["permanent"].as_object_mut().unwrap();
                    if let Some(a) = attached.get(&k) {
                        perm.insert("attached_to".into(), a.clone());
                    }
                    if attackers.contains(&k) {
                        perm.insert("attacking".into(), json!(true));
                        perm.insert("attack_target".into(), json!({"player": SEATS[1 - ctl]}));
                    }
                    if let Some(bl) = blocks.get(&k) {
                        perm.insert("blocking".into(), json!(true));
                        perm.insert("blocked_attackers".into(), json!(bl));
                    }
                }
            }
            for zone in [&mut graveyard, &mut exile] {
                for side in zone.iter_mut() {
                    for rec in side.iter_mut() {
                        let id = rec["object_id"].clone();
                        // find the key by id
                        if let Some((k, _)) = view.records.iter().find(|(_, r)| r["object_id"] == id) {
                            if let Some(by) = exiled_by.get(k) {
                                rec["exiled_by"] = by.clone();
                            }
                        }
                    }
                }
            }
        }
        let initiative = p.initiative.map(seat_of);
        let passes = p.engine_context.priority_passes;
        let players: Vec<Value> = (0..2)
            .map(|i| {
                let pool = p.mana_pools[i];
                json!({
                    "seat": SEATS[i],
                    "life": p.life_totals[i],
                    "poison": null,
                    "counters": null,
                    "mana_pool": {"W": pool[0], "U": pool[1], "B": pool[2], "R": pool[3], "G": pool[4], "C": pool[5]},
                    "lands_played_this_turn": p.player_status[i].lands_played_this_turn as u32,
                    "mulligans_taken": 0,
                    "designations": if initiative == Some(i) { json!(["initiative"]) } else { json!([]) },
                    "progress": null,
                    "hand_count": p.hand_counts[i] as u64,
                    "library_count": p.library_counts[i] as u64,
                    "hand": if i == viewer { json!(hand) } else { Value::Null },
                    "battlefield": battlefield[i],
                    "graveyard": graveyard[i],
                    "exile": exile[i],
                    "command": [],
                })
            })
            .collect();
        let ctx_stage = p.engine_context.current_stage;
        let priority_seat = if priority {
            json!(SEATS[viewer])
        } else {
            match ctx_stage {
                EngineDecisionStageV2::PendingCast | EngineDecisionStageV2::PendingActivation => {
                    json!(SEATS[seat_of(p.priority_player)])
                }
                _ => Value::Null,
            }
        };
        let mut o = Map::new();
        o.insert("viewer".into(), json!(SEATS[viewer]));
        o.insert("turn".into(), json!(p.turn));
        o.insert("phase_step".into(), json!(phase(p.phase)));
        o.insert("active_seat".into(), json!(SEATS[seat_of(p.active_player)]));
        o.insert("priority_seat".into(), priority_seat);
        o.insert(
            "passed_seats".into(),
            json!((0..2).filter(|i| passes[*i]).map(|i| SEATS[i]).collect::<Vec<_>>()),
        );
        o.insert("day_night".into(), Value::Null);
        o.insert("players".into(), Value::Array(players));
        o.insert("stack".into(), Value::Array(stack));
        o.insert("pending_triggers".into(), Value::Null);
        o.insert("known".into(), json!([]));
        (o, view)
    }

    /// `known` (spec 6.7): the kernel's positional knowledge plus every
    /// hidden-zone card a candidate references, ids only on current looks.
    fn finish_known(&mut self, obs: &ObservationV5, o: &mut Map<String, Value>, view: &mut View) -> Result<(), String> {
        let viewer = view.viewer;
        let p = &obs.projection.surface;
        let id_key = self.id_key;
        let epoch = view.look_epoch;
        let mut entries: Vec<Value> = Vec::new();
        let mut covered: HashSet<String> = HashSet::new();
        let mut ided = view.look_ided.clone();
        for (owner, list) in obs.known_library_cards.iter().enumerate() {
            for k in list {
                if (k.position as usize) >= p.library_counts[owner] {
                    continue;
                }
                let key = ckey(&k.card.stable);
                let referenced = view.hidden.contains_key(&key) || ided.contains(&key);
                let how = if owner == viewer { "looked_at" } else { "revealed" };
                let id = if referenced {
                    ided.insert(key.clone());
                    covered.insert(key.clone());
                    Value::String(object_id(&id_key, viewer, &format!("{key}:look:{epoch}")))
                } else {
                    Value::Null
                };
                entries.push(json!({
                    "owner_seat": SEATS[owner], "zone": "library", "card_name": name_of(k.card.stable.card_db_id),
                    "object_id": id, "position_from_top": k.position, "position_from_bottom": null, "how": how,
                }));
            }
        }
        for (owner, list) in obs.known_hand_cards.iter().enumerate() {
            if owner == viewer {
                continue;
            }
            let mut n = 0usize;
            for c in list {
                if n >= p.hand_counts[owner] {
                    break;
                }
                n += 1;
                let key = ckey(&c.stable);
                let referenced = view.hidden.contains_key(&key) || ided.contains(&key);
                let id = if referenced {
                    ided.insert(key.clone());
                    covered.insert(key.clone());
                    Value::String(object_id(&id_key, viewer, &format!("{key}:look:{epoch}")))
                } else {
                    Value::Null
                };
                entries.push(json!({
                    "owner_seat": SEATS[owner], "zone": "hand", "card_name": name_of(c.stable.card_db_id),
                    "object_id": id, "position_from_top": null, "position_from_bottom": null, "how": "revealed",
                }));
            }
        }
        for (key, (r, v)) in &view.hidden {
            if covered.contains(key) {
                continue;
            }
            ided.insert(key.clone());
            let owner = seat_of(r.owner);
            let (zone, how) = if r.zone == Zone::Library { ("library", "searching") } else { ("hand", "revealed") };
            if zone == "hand" && entries.iter().filter(|e| e["zone"] == "hand" && e["owner_seat"] == SEATS[owner]).count() >= p.hand_counts[owner] {
                return Err("a referenced hand card exceeds hand_count".into());
            }
            entries.push(json!({
                "owner_seat": SEATS[owner], "zone": zone, "card_name": v["card_name"],
                "object_id": v["object_id"], "position_from_top": null, "position_from_bottom": null, "how": how,
            }));
        }
        let key = |e: &Value| -> (String, String, String, (u8, u64), (u8, u64), String, (u8, String)) {
            let pos = |f: &str| match e[f].as_u64() {
                None => (0, 0),
                Some(v) => (1, v),
            };
            (
                e["owner_seat"].as_str().unwrap().to_string(),
                e["zone"].as_str().unwrap().to_string(),
                e["card_name"].as_str().unwrap_or("").to_string(),
                pos("position_from_top"),
                pos("position_from_bottom"),
                e["how"].as_str().unwrap().to_string(),
                match e["object_id"].as_str() {
                    None => (0, String::new()),
                    Some(s) => (1, s.to_string()),
                },
            )
        };
        entries.sort_by_key(|e| key(e));
        o.insert("known".into(), Value::Array(entries));
        self.look[viewer].ided = ided;
        Ok(())
    }
}

fn short_reason(why: &str) -> String {
    let s: String = why
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() { c.to_ascii_lowercase() } else { '_' })
        .collect();
    let s = s.trim_matches('_').to_string();
    s.chars().take(80).collect()
}

fn kernel_kind(s: &ActionSemanticV1) -> &'static str {
    use ActionSemanticV1::*;
    match s {
        Pass { .. } => "pass",
        PlayLand { .. } => "play_land",
        CastSpell { .. } => "cast_spell",
        ActivateManaAbility { .. } => "activate_mana_ability",
        ActivateAbility { .. } => "activate_ability",
        PlotSpell { .. } => "plot_spell",
        _ => "choice",
    }
}

fn cost_kind_word(k: CostKind) -> &'static str {
    match k {
        CostKind::SacrificeLands
        | CostKind::SacrificePermanents
        | CostKind::SacrificeCreatures
        | CostKind::SacrificeArtifacts => "sacrifice",
        CostKind::DiscardCards => "discard",
        CostKind::ExileFromGraveyard => "exile",
        CostKind::TapPermanents => "tap",
        CostKind::ReturnPermanentsToHand => "return_to_hand",
        CostKind::RemoveCounters => "remove_counter",
        _ => "other",
    }
}

fn optional_cost_choice_word(c: OptionalCostChoice) -> &'static str {
    match c {
        OptionalCostChoice::Decline => "decline",
        OptionalCostChoice::Discard => "discard",
        OptionalCostChoice::SacrificeLand => "sacrifice_land",
        OptionalCostChoice::ReturnPermanent => "return_permanent",
    }
}

/// The source the kernel's pending stage names, for kinds whose action
/// carries none.
fn pending_source(obs: &ObservationV5) -> Option<CardStableRefV1> {
    let ec = &obs.projection.surface.engine_context;
    match ec.current_stage {
        EngineDecisionStageV2::PendingCast => ec.pending_cast.as_ref().and_then(|p| p.source.clone()),
        EngineDecisionStageV2::PendingActivation => ec.pending_activation.as_ref().and_then(|p| p.source.clone()),
        EngineDecisionStageV2::PendingOptionalCost => ec
            .pending_optional_cost
            .as_ref()
            .and_then(|p| p.source.clone().or_else(|| p.spell_resume_source.clone())),
        EngineDecisionStageV2::PendingOptionalCostSacrifice => ec
            .pending_optional_cost_sacrifice
            .as_ref()
            .and_then(|p| p.source.clone().or_else(|| p.spell_resume_source.clone())),
        EngineDecisionStageV2::PendingDiscard => ec.pending_discard.as_ref().and_then(|p| p.resume_source.clone()),
        EngineDecisionStageV2::PendingEffect => ec.pending_effect.as_ref().and_then(|p| p.source.clone()),
        _ => None,
    }
}

// ---------------------------------------------------------------------------
// The bridge
// ---------------------------------------------------------------------------

struct Bridge {
    last: Option<(String, Vec<u8>, String)>,
    game: Option<Game>,
    used_game_ids: HashSet<String>,
    episodes: u64,
    halts: u64,
    subtype_names: HashMap<u16, String>,
}

impl Bridge {
    fn new() -> Self {
        let mut subtype_names = HashMap::new();
        for def in CARD_DEFS.iter() {
            for s in def.subtypes {
                subtype_names.insert(*s as u16, subtype_name(*s));
            }
        }
        for s in Subtype::CREATURE_TYPES {
            subtype_names.insert(*s as u16, subtype_name(*s));
        }
        Bridge {
            last: None,
            game: None,
            used_game_ids: HashSet::new(),
            episodes: 0,
            halts: 0,
            subtype_names,
        }
    }

    fn handle(&mut self, raw: &[u8]) -> String {
        if raw.len() > MAX_LINE {
            return canonical(&error_response("", &err("malformed_json", "line exceeds 8 MiB")));
        }
        let value = match parse_strict(raw) {
            Ok(v) => v,
            Err(e) => return canonical(&error_response("", &err("malformed_json", e))),
        };
        let Some(m) = value.as_object() else {
            return canonical(&error_response("", &malformed("top level must be an object")));
        };
        let request_id = match m.get("request_id").and_then(Value::as_str) {
            Some(s) if !s.is_empty() => s.to_string(),
            _ => return canonical(&error_response("", &malformed("request_id must be a nonempty string"))),
        };
        match m.get("protocol") {
            Some(Value::String(p)) if p == PROTOCOL => {}
            Some(Value::String(_)) => {
                return canonical(&error_response(&request_id, &err("protocol_mismatch", "protocol must be spellbench/v2")))
            }
            _ => return canonical(&error_response(&request_id, &malformed("protocol must be a string"))),
        }
        let rt = m.get("request_type").and_then(Value::as_str).unwrap_or("");
        // Strict parse per request type; failures are never cached (spec 4.1).
        let parsed: Result<(), Err> = match rt {
            "hello" => exact_keys(m, &["request_type", "protocol", "request_id", "protocol_minor"], "hello")
                .and_then(|_| u32_field(m, "protocol_minor").map(|_| ())),
            "reset" => parse_reset(m).map(|_| ()),
            "step" => parse_step(m).map(|_| ()),
            "validate_deck" => parse_validate_deck(m).map(|_| ()),
            "probe_resample" => exact_keys(m, &["request_type", "protocol", "request_id", "game_id", "samples"], "probe_resample")
                .and_then(|_| nonempty_str(m, "game_id").map(|_| ()))
                .and_then(|_| u32_field(m, "samples").map(|_| ())),
            other => Err(malformed(format!("unknown request_type {other:?}"))),
        };
        if let Err(e) = parsed {
            return canonical(&error_response(&request_id, &e));
        }
        if let Some((id, line, resp)) = &self.last {
            if *id == request_id {
                if line.as_slice() == raw {
                    return resp.clone();
                }
                return canonical(&error_response(
                    &request_id,
                    &err("request_id_reuse_mismatch", "request_id was just used with another payload"),
                ));
            }
        }
        let response = match self.dispatch(&request_id, rt, m) {
            Ok(v) => v,
            Err(e) => {
                eprintln!("agent_bridge_v2: error {}: {}", e.code, e.message);
                error_response(&request_id, &e)
            }
        };
        let out = canonical(&response);
        self.last = Some((request_id, raw.to_vec(), out.clone()));
        out
    }

    fn dispatch(&mut self, request_id: &str, rt: &str, m: &Map<String, Value>) -> Result<Value, Err> {
        match rt {
            "hello" => Ok(hello_ok(request_id)),
            "reset" => self.reset(request_id, m),
            "step" => self.step(request_id, m),
            "validate_deck" => {
                let (format, catalog) = parse_validate_deck(m)?;
                if format != FORMAT {
                    return Err(err("unsupported_format", format!("format {format:?} is not served")));
                }
                match catalog {
                    None => Err(err("unsupported_deck", "this engine takes catalog decks only")),
                    Some(c) if catalog_rows(&c).is_none() => Err(err("unsupported_deck", format!("unknown catalog_id {c:?}"))),
                    Some(_) => {
                        let m = envelope("deck_ok", request_id);
                        Ok(Value::Object(m))
                    }
                }
            }
            "probe_resample" => Err(err("unsupported_request", "this engine has no probe (spec 9.7)")),
            _ => Err(malformed("unknown request_type")),
        }
    }

    fn reset(&mut self, request_id: &str, m: &Map<String, Value>) -> Result<Value, Err> {
        let req = parse_reset(m)?;
        if self.used_game_ids.contains(&req.game_id) {
            return Err(malformed(format!("game_id {:?} was used by an earlier reset", req.game_id)));
        }
        if let Some(g) = &self.game {
            if !g.over {
                return Err(err("game_already_active", format!("game {:?} is still active", g.game_id)));
            }
        }
        if req.format != FORMAT {
            return Err(err("unsupported_format", format!("format {:?} is not served", req.format)));
        }
        let mut mainboards: [Vec<u16>; 2] = Default::default();
        let mut labels: [String; 2] = Default::default();
        for (i, (deck_id, catalog, is_list)) in req.decks.iter().enumerate() {
            if *is_list {
                return Err(err("unsupported_deck", format!("the {} deck: catalog decks only", SEATS[i])));
            }
            let c = catalog.as_ref().unwrap();
            let Some(deck) = runtime_deck_by_id(c).filter(|d| d.is_fully_materialized()) else {
                return Err(err("unsupported_deck", format!("the {} deck: unknown catalog_id {c:?}", SEATS[i])));
            };
            let _ = deck_id;
            mainboards[i] = deck.card_ids.to_vec();
            labels[i] = c.clone();
        }
        for (i, (deck_id, catalog, _)) in req.decks.iter().enumerate() {
            let expected = deck_id_of(&catalog_rows(catalog.as_ref().unwrap()).unwrap());
            if *deck_id != expected {
                return Err(err("deck_id_mismatch", format!("the {} deck_id does not match its list ({expected})", SEATS[i])));
            }
        }
        let r = &req.rules;
        if r["mulligan"].as_str() != Some("none") {
            return Err(err("unsupported_rule", "rules.mulligan must be none (rules_supported)"));
        }
        if r["starting_player"].as_str() != Some("host_assigned") {
            return Err(err("unsupported_rule", "rules.starting_player must be host_assigned (rules_supported)"));
        }
        if !r["extensions"].as_array().map(|a| a.is_empty()).unwrap_or(false) {
            return Err(err("unsupported_rule", "rules.extensions: this engine declares no extension"));
        }
        if r["probe"].as_bool() == Some(true) {
            return Err(err("unsupported_rule", "rules.probe: this engine has no probe"));
        }
        let starting = if r["starting_seat"].as_str() == Some("p1") { PlayerId::P1 } else { PlayerId::P0 };
        let root = hmac(&req.secret, b"spellbench/v2/rng:shared:kernel_environment_v2:0");
        let pair_seed = u64::from_be_bytes(root[..8].try_into().unwrap());
        let id_key = hmac(&req.secret, b"spellbench/v2/object-id");
        self.episodes += 1;
        let session = RlEpisodeSessionV1::reset_with_explicit_decks_and_limits_with_starting_player_v1(
            self.episodes,
            pair_seed,
            u64::MAX / 4,
            u64::MAX / 4,
            labels,
            mainboards,
            starting,
        )
        .map_err(|e| err("unsupported_deck", format!("{e:?}")))?;
        self.used_game_ids.insert(req.game_id.clone());
        self.game = Some(Game {
            game_id: req.game_id,
            episode: self.episodes,
            session,
            id_key,
            max_steps: req.max_steps,
            max_decisions: req.max_decisions,
            step: 0,
            seat_steps: [0, 0],
            next_group: [0, 0],
            partial: [None, None],
            completed: 0,
            pending: None,
            over: false,
            abilities: Vec::new(),
            next_serial: 0,
            look: Default::default(),
            seq: [None, None],
            order: None,
            subtype_names: self.subtype_names.clone(),
        });
        Ok(self.produce(request_id))
    }

    fn step(&mut self, request_id: &str, m: &Map<String, Value>) -> Result<Value, Err> {
        let (game_id, expected, cid, echo) = parse_step(m)?;
        let Some(game) = self.game.as_mut() else {
            return Err(err("step_before_reset", "no game has been reset"));
        };
        if game.game_id != game_id {
            return Err(err("game_id_mismatch", format!("this engine's game is {:?}", game.game_id)));
        }
        if game.over {
            return Err(err("game_already_terminal", "the game has ended"));
        }
        if expected != game.step {
            return Err(err("expected_step_mismatch", format!("the pending decision's step is {}", game.step)));
        }
        let pending = game.pending.as_ref().expect("live game has a pending decision");
        if cid as usize >= pending.semantics.len() {
            return Err(err(
                "candidate_id_out_of_range",
                format!("candidate_id {cid} is outside the {} candidates", pending.semantics.len()),
            ));
        }
        if echo != pending.semantics[cid as usize] {
            return Err(err("semantic_echo_mismatch", format!("semantic_echo differs from candidate {cid}")));
        }
        let pending = game.pending.take().unwrap();
        let seat = pending.seat;
        // Account the answer (spec 8, 9.3).
        game.step += 1;
        game.seat_steps[seat] += 1;
        let (gid, idx, cnt) = pending.group;
        if idx + 1 == cnt {
            game.completed += 1;
            game.next_group[seat] = gid + 1;
            game.partial[seat] = None;
        } else {
            game.partial[seat] = Some((gid, idx, cnt));
        }
        let chosen_kind = pending.kinds[cid as usize].clone();
        game.seq[seat] = match (&pending.seq_tag, chosen_kind.as_str()) {
            (Some(tag), k) if !k.starts_with("finish") => {
                let prev = match &game.seq[seat] {
                    Some((t, n)) if t == tag => *n,
                    _ => 0,
                };
                Some((tag.clone(), prev + 1))
            }
            _ => None,
        };
        match pending.answers[cid as usize].clone() {
            Answer::OrderItem(item) => {
                let order = game.order.as_mut().expect("order state");
                order.picked.push(item);
                if order.picked.len() + 1 < order.n {
                    return Ok(self.produce(request_id));
                }
                let mut perm = order.picked.clone();
                let last = (0..order.n).find(|i| !perm.contains(i)).unwrap();
                perm.push(last);
                let Some(k) = order.perms.iter().position(|p| *p == perm) else {
                    let g = self.game.as_mut().unwrap();
                    self.halts += 1;
                    return Ok(g.halted(request_id, "trigger order has no kernel permutation"));
                };
                game.order = None;
                return Ok(self.kernel_step(request_id, pending.kernel_step, k));
            }
            Answer::Kernel(k) => Ok(self.kernel_step(request_id, pending.kernel_step, k)),
        }
    }

    fn kernel_step(&mut self, request_id: &str, kernel_step: u64, index: usize) -> Value {
        let game = self.game.as_mut().unwrap();
        let RlSessionResponseV1::Decision(d) = game.session.current_response() else {
            self.halts += 1;
            return game.halted(request_id, "kernel has no pending decision");
        };
        if d.step != kernel_step {
            self.halts += 1;
            return game.halted(request_id, "kernel step drifted");
        }
        let a = &d.legal_actions[index];
        match game.session.step(game.episode, d.step, a.selected_index, &a.stable_id) {
            Ok(_) => self.produce(request_id),
            Err(e) => {
                self.halts += 1;
                game.halted(request_id, &format!("kernel refused step: {e:?}"))
            }
        }
    }

    fn produce(&mut self, request_id: &str) -> Value {
        let game = self.game.as_mut().unwrap();
        match game.produce(request_id) {
            Ok(v) => v,
            Err(why) => {
                self.halts += 1;
                game.halted(request_id, &why)
            }
        }
    }
}

fn parse_step(m: &Map<String, Value>) -> Result<(String, u64, u64, Value), Err> {
    exact_keys(m, &["request_type", "protocol", "request_id", "game_id", "expected_step", "selection"], "step")?;
    let game_id = nonempty_str(m, "game_id")?.to_string();
    let expected = safe_int(m, "expected_step")?;
    let sel = obj(&m["selection"], "selection")?;
    exact_keys(sel, &["candidate_id", "semantic_echo"], "selection")?;
    let cid = u64::from(u32_field(sel, "candidate_id")?);
    if !sel["semantic_echo"].is_object() {
        return Err(malformed("selection.semantic_echo must be an object"));
    }
    Ok((game_id, expected, cid, sel["semantic_echo"].clone()))
}

fn parse_validate_deck(m: &Map<String, Value>) -> Result<(String, Option<String>), Err> {
    exact_keys(m, &["request_type", "protocol", "request_id", "format", "deck"], "validate_deck")?;
    let format = nonempty_str(m, "format")?.to_string();
    let d = obj(&m["deck"], "deck")?;
    let keys: BTreeSet<&str> = d.keys().map(String::as_str).collect();
    if keys == BTreeSet::from(["catalog_id"]) {
        Ok((format, Some(nonempty_str(d, "catalog_id")?.to_string())))
    } else if keys == BTreeSet::from(["decklist"]) {
        check_decklist(&d["decklist"], "deck.decklist")?;
        Ok((format, None))
    } else {
        Err(malformed("deck must be exactly {catalog_id} or {decklist}"))
    }
}

// ---------------------------------------------------------------------------
// Decision production
// ---------------------------------------------------------------------------

impl Game {
    fn produce(&mut self, request_id: &str) -> Result<Value, String> {
        let d = match self.session.current_response() {
            RlSessionResponseV1::Terminal(t) => {
                let (outcome, winner) = match t.terminal_outcome {
                    TerminalOutcomeV1::P0Win => ("p0_win", Some("p0")),
                    TerminalOutcomeV1::P1Win => ("p1_win", Some("p1")),
                    TerminalOutcomeV1::Draw => ("draw", None),
                    TerminalOutcomeV1::Truncated => ("truncated", None),
                    TerminalOutcomeV1::Halted => ("halted", None),
                };
                let classification = match outcome {
                    "truncated" => "truncated",
                    "halted" => "halted",
                    _ => "natural",
                };
                // Reasons name no engine-internal identity (spec 9.5, F2).
                let reason = match classification {
                    "natural" => "game_over".to_string(),
                    _ => {
                        let parts: Vec<&str> = t.terminal_reason.split(':').take(2).collect();
                        eprintln!("agent_bridge_v2: kernel terminal {} ({})", classification, t.terminal_reason);
                        format!("kernel_{}", short_reason(&parts.join("_")))
                    }
                };
                if self.partial.iter().any(Option::is_some) && classification == "natural" {
                    return Err("natural terminal inside a partial group".into());
                }
                return Ok(self.end(request_id, outcome, classification, winner, &reason));
            }
            RlSessionResponseV1::Decision(d) => d,
        };
        let seat = seat_of(d.acting_player);
        // Caps (spec 9.2): checked before posing.
        if self.step >= self.max_steps {
            return Ok(self.end(request_id, "truncated", "truncated", None, "max_steps"));
        }
        let starts_group = match (&self.order, d.substep_index) {
            (Some(o), _) => o.picked.is_empty(),
            (None, 0) => true,
            _ => false,
        };
        if starts_group && self.partial[seat].is_none() && self.completed >= self.max_decisions {
            return Ok(self.end(request_id, "truncated", "truncated", None, "max_decisions"));
        }
        if self.partial[1 - seat].is_some() {
            return Err("decision for the other seat inside a partial group".into());
        }
        let has_order = d
            .legal_actions
            .iter()
            .any(|a| matches!(a.semantic, ActionSemanticV1::OrderTriggers { .. }));
        let sd = if has_order {
            self.order_decision(&d, seat)?
        } else {
            self.plain_decision(&d, seat)?
        };
        let mut m = envelope("decision", request_id);
        m.insert("game_id".into(), json!(self.game_id));
        m.insert("step".into(), json!(self.step));
        m.insert("seat_decision".into(), sd);
        m.insert("provenance".into(), provenance());
        Ok(Value::Object(m))
    }

    fn group_for(&self, seat: usize, idx: u32, cnt: u32) -> Result<(u64, u32, u32), String> {
        if cnt <= 1 {
            if self.partial[seat].is_some() {
                return Err("single decision inside a partial group".into());
            }
            return Ok((self.next_group[seat], 0, 1));
        }
        match self.partial[seat] {
            None if idx == 0 => Ok((self.next_group[seat], 0, cnt)),
            Some((gid, pi, pc)) if pi + 1 == idx && pc == cnt => Ok((gid, idx, cnt)),
            _ => Err(format!("kernel substep {idx}/{cnt} does not continue the seat's group")),
        }
    }

    fn assemble(
        &mut self,
        d: &RlSessionDecisionV1,
        seat: usize,
        group: (u64, u32, u32),
        mut o: Map<String, Value>,
        mut view: View,
        mut cands: Vec<(Value, Answer)>,
        priority: bool,
        seq_tag: Option<String>,
    ) -> Result<Value, String> {
        if cands.is_empty() {
            return Err("decision with no candidates".into());
        }
        if cands.len() > 4096 {
            return Err("candidate_limit".into());
        }
        self.finish_known(&d.observation, &mut o, &mut view)?;
        if !view.fallbacks.is_empty() {
            eprintln!("agent_bridge_v2: ref fallback {:?}", view.fallbacks);
        }
        // pass first (spec 7.1)
        if let Some(i) = cands.iter().position(|(s, _)| s["kind"] == "pass") {
            let c = cands.remove(i);
            cands.insert(0, c);
        }
        // hidden-zone candidates in (card_name, object_id) order (spec 7.1, V5)
        let hidden_key = |s: &Value| -> Option<Vec<(String, String)>> {
            let mut out = Vec::new();
            collect_hidden(s, SEATS[seat], &mut out);
            if out.is_empty() { None } else { Some(out) }
        };
        let slots: Vec<usize> = (0..cands.len()).filter(|i| hidden_key(&cands[*i].0).is_some()).collect();
        if slots.len() > 1 {
            let mut items: Vec<(Value, Answer)> = slots.iter().map(|i| cands[*i].clone()).collect();
            items.sort_by(|a, b| hidden_key(&a.0).cmp(&hidden_key(&b.0)));
            for (slot, item) in slots.iter().zip(items) {
                cands[*slot] = item;
            }
        }
        // distinct semantics (spec 7.1)
        let mut seen = HashSet::new();
        for (s, _) in &cands {
            if !seen.insert(canonical(s)) {
                return Err(format!("duplicate candidate semantic {}", canonical(s)));
            }
        }
        // context (spec 9.3)
        let sources: Vec<&Value> = cands.iter().map(|(s, _)| s.get("source").unwrap_or(&Value::Null)).collect();
        let ctx_source = if !priority && !sources.is_empty() && sources.iter().all(|s| *s == sources[0] && !s.is_null()) {
            sources[0].clone()
        } else {
            Value::Null
        };
        let purposes: BTreeSet<String> = cands
            .iter()
            .filter_map(|(s, _)| s.get("purpose").and_then(Value::as_str).map(String::from))
            .collect();
        let ctx_purpose = if purposes.len() == 1 { json!(purposes.iter().next().unwrap()) } else { Value::Null };
        let kinds: Vec<String> = cands.iter().map(|(s, _)| s["kind"].as_str().unwrap().to_string()).collect();
        let candidates: Vec<Value> = cands
            .iter()
            .enumerate()
            .map(|(i, (s, _))| json!({"candidate_id": i, "semantic": s, "display_text": null}))
            .collect();
        let sd = json!({
            "acting_seat": SEATS[seat],
            "seat_step": self.seat_steps[seat],
            "group": {"group_id": group.0, "substep_index": group.1, "substep_count": group.2},
            "context": {"kind": if priority { "priority" } else { "choice" }, "source": ctx_source,
                        "purpose": ctx_purpose, "text": null, "rewind": false},
            "observation": Value::Object(o),
            "candidates": candidates,
            "extensions": {},
        });
        self.pending = Some(Pending {
            seat,
            semantics: cands.iter().map(|(s, _)| s.clone()).collect(),
            answers: cands.into_iter().map(|(_, a)| a).collect(),
            group,
            kernel_step: d.step,
            seq_tag,
            kinds,
        });
        Ok(sd)
    }

    fn order_decision(&mut self, d: &RlSessionDecisionV1, seat: usize) -> Result<Value, String> {
        if self.order.is_none() {
            let mut perms = Vec::new();
            let mut sources: Option<Vec<CardStableRefV1>> = None;
            for a in &d.legal_actions {
                let ActionSemanticV1::OrderTriggers { pending_sources, order, .. } = &a.semantic else {
                    return Err("order_triggers mixed with other kinds".into());
                };
                if sources.is_none() {
                    sources = Some(pending_sources.clone());
                }
                perms.push(order.clone());
            }
            let n = sources.as_ref().map(|s| s.len()).unwrap_or(0);
            if n < 2 {
                return Err("order_triggers with fewer than two triggers".into());
            }
            if self.partial[seat].is_some() {
                return Err("trigger ordering inside a partial group".into());
            }
            self.order = Some(OrderState {
                seat,
                n,
                perms,
                picked: Vec::new(),
                group_id: self.next_group[seat],
            });
        }
        let (o, mut view) = self.observe(&d.observation, seat, false);
        let order = self.order.as_ref().unwrap();
        if order.seat != seat {
            return Err("trigger order seat changed".into());
        }
        let ActionSemanticV1::OrderTriggers { pending_sources, .. } = &d.legal_actions[0].semantic else {
            unreachable!()
        };
        let pos = order.picked.len();
        let group = (order.group_id, pos as u32, (order.n - 1) as u32);
        let n = order.n;
        let picked = order.picked.clone();
        let perms = order.perms.clone();
        // instance numbering of otherwise identical triggers
        let mut instance = vec![0u32; n];
        for i in 0..n {
            instance[i] = (0..i).filter(|j| pending_sources[*j] == pending_sources[i]).count() as u32;
        }
        let id_key = self.id_key;
        let mut cands = Vec::new();
        for item in 0..n {
            if picked.contains(&item) {
                continue;
            }
            let mut prefix = picked.clone();
            prefix.push(item);
            if !perms.iter().any(|p| p.starts_with(&prefix)) {
                continue;
            }
            let src = view.reference(&pending_sources[item], &id_key);
            let name = name_of(pending_sources[item].card_db_id);
            let sem = json!({
                "kind": "order_pick",
                "source": null,
                "purpose": "triggers",
                "item": {"trigger": {"source": src, "source_name": name, "ability_index": null,
                                     "event_objects": [], "instance": instance[item], "label": null}},
                "position": pos as u32,
                "count": n as u32,
            });
            cands.push((sem, Answer::OrderItem(item)));
        }
        let _ = view.hidden.len();
        self.assemble(d, seat, group, o, view, cands, false, None)
    }

    fn plain_decision(&mut self, d: &RlSessionDecisionV1, seat: usize) -> Result<Value, String> {
        let kinds: Vec<&str> = d.legal_actions.iter().map(|a| kernel_kind(&a.semantic)).collect();
        let priority = kinds.iter().any(|k| *k != "choice");
        if priority && kinds.iter().any(|k| *k == "choice") {
            return Err("priority kinds mixed with choice kinds".into());
        }
        let group = self.group_for(seat, d.substep_index, d.substep_count)?;
        let obs = &d.observation;
        let (o, mut view) = self.observe(obs, seat, priority);
        let id_key = self.id_key;
        let opp = SEATS[1 - seat];
        let ec = &obs.projection.surface.engine_context;
        let stage = ec.current_stage;
        let pending_src = pending_source(obs);
        // Selection counts for kinds whose action carries none.
        let seq_tag_for = |s: &ActionSemanticV1| -> Option<String> {
            use ActionSemanticV1::*;
            match s {
                ChooseTarget { source, .. } => Some(format!("target:{}", ckey(source))),
                ChooseCostTarget { source, cost_kind, .. } => Some(format!("cost:{:?}:{}", cost_kind, ckey(source))),
                Discard { .. } => Some(format!("discard:{stage:?}")),
                _ => None,
            }
        };
        let seq_tag = d.legal_actions.iter().find_map(|a| seq_tag_for(&a.semantic));
        let seq_count = match (&seq_tag, &self.seq[seat]) {
            (Some(t), Some((pt, n))) if t == pt => *n,
            _ => 0,
        };
        let finish_count: Option<u32> = d.legal_actions.iter().find_map(|a| match &a.semantic {
            ActionSemanticV1::FinishTargetSelection { selected_count, .. } => Some(u32::from(*selected_count)),
            _ => None,
        });
        let effect_choice = ec.pending_effect.as_ref().and_then(|p| p.choice.clone());
        let (target_purpose, bool_purpose) = match &effect_choice {
            Some(PendingEffectChoiceSemanticV4::Targets { purpose, .. }) => (Some(*purpose), None),
            Some(PendingEffectChoiceSemanticV4::Boolean { purpose, .. }) => (None, Some(*purpose)),
            _ => (None, None),
        };
        let remaining_discard: Option<u32> = obs
            .projection
            .surface
            .surface_context
            .private_discard
            .as_ref()
            .map(|p| p.remaining_needed);
        // Cast method lookahead (spec 7.2: null when choose_cast_method follows).
        let mut cands: Vec<(Value, Answer)> = Vec::new();
        let mut mana_seen: HashMap<String, u32> = HashMap::new();
        let required = |v: Option<Value>, what: &str| -> Result<Value, String> {
            v.ok_or_else(|| format!("unresolvable {what} reference"))
        };
        for (i, a) in d.legal_actions.iter().enumerate() {
            use ActionSemanticV1::*;
            let sem = match &a.semantic {
                Pass { .. } => json!({"kind": "pass"}),
                PlayLand { source, .. } => {
                    json!({"kind": "play_land", "source": required(view.reference(source, &id_key), "play_land.source")?, "face": 0})
                }
                CastSpell { source, .. } => {
                    let method = self.cast_method(d, i, source);
                    json!({"kind": "cast_spell", "source": required(view.reference(source, &id_key), "cast_spell.source")?, "method": method})
                }
                ActivateManaAbility { source, mana_choice, cost_target, .. } => {
                    let src = required(view.reference(source, &id_key), "activate_mana_ability.source")?;
                    let ct = match cost_target {
                        None => Value::Null,
                        Some(t) => json!({"object": required(view.reference(t, &id_key), "cost_target")?}),
                    };
                    // The kernel names a colour only when the ability offers a
                    // choice; a single-colour source's symbol is filled in, as
                    // gorge's own v2 engine does (spec 7.2: a symbol or null).
                    let produced = def_of(source.card_db_id).produces_mana;
                    let mc = match mana_choice {
                        Some(c) => json!(mana_symbol(*c)),
                        None if produced.len() == 1 => json!(mana_symbol(produced[0])),
                        None => Value::Null,
                    };
                    let dup_key = canonical(&json!([src, mc, ct]));
                    let idx = mana_seen.entry(dup_key).or_insert(0);
                    let ability_index = *idx;
                    *idx += 1;
                    json!({"kind": "activate_mana_ability", "source": src, "ability_index": ability_index,
                           "mana_choice": mc, "cost_target": ct})
                }
                ActivateAbility { source, ability_index, .. } => json!({"kind": "activate_ability",
                    "source": required(view.reference(source, &id_key), "activate_ability.source")?, "ability_index": *ability_index as u32}),
                PlotSpell { source, .. } => json!({"kind": "special_action",
                    "source": required(view.reference(source, &id_key), "special_action.source")?, "action": "plot"}),
                ChooseTarget { source, remaining, target, .. } => {
                    let sel = finish_count.unwrap_or(seq_count);
                    let max = sel + u32::from(*remaining);
                    let min = if finish_count.is_some() { sel } else { max };
                    json!({"kind": "choose_target", "source": required(view.reference(source, &id_key), "choose_target.source")?,
                           "slot": 0, "target": required(view.target(target, &id_key), "choose_target.target")?,
                           "selected_count": sel, "minimum": min, "maximum": max})
                }
                FinishTargetSelection { source, selected_count, .. } => json!({"kind": "finish_target_selection",
                    "source": required(view.reference(source, &id_key), "finish_target_selection.source")?, "slot": 0,
                    "selected_count": u32::from(*selected_count)}),
                ChooseCostTarget { source, cost_kind, remaining, candidate, .. } => {
                    let max = seq_count + u32::from(*remaining);
                    json!({"kind": "choose_cost_target", "source": required(view.reference(source, &id_key), "choose_cost_target.source")?,
                           "cost_kind": cost_kind_word(*cost_kind),
                           "candidate": required(view.reference(candidate, &id_key), "choose_cost_target.candidate")?,
                           "selected_count": seq_count, "minimum": max, "maximum": max})
                }
                ChooseCastMode { source, mode, .. } => json!({"kind": "choose_cast_method",
                    "source": required(view.reference(source, &id_key), "choose_cast_method.source")?,
                    "method": match mode { CastMode::Normal => "normal", CastMode::Alternative => "alternative" }}),
                ChooseKicker { source, pay, .. } => json!({"kind": "optional_cost",
                    "source": required(view.reference(source, &id_key), "optional_cost.source")?, "cost": "kicker", "pay": pay}),
                ChooseSpellMode { source, mode_index, mode_count, .. } => json!({"kind": "choose_spell_mode",
                    "source": required(view.reference(source, &id_key), "choose_spell_mode.source")?,
                    "mode_index": *mode_index as u32, "mode_count": *mode_count as u32,
                    "selected_count": 0, "minimum": 1, "maximum": 1}),
                ChooseEffectOption { source, option_index, option_count, .. } => json!({"kind": "choose_option",
                    "source": view.reference(source, &id_key), "purpose": "effect_option",
                    "option_index": *option_index as u32, "option_count": *option_count as u32, "option_label": null}),
                ChooseEffectTarget { source, target, selected_count, min_targets, max_targets, .. } => {
                    let sel = u32::from(*selected_count);
                    let (min, max) = (u32::from(*min_targets), u32::from(*max_targets));
                    let t = required(view.target(target, &id_key), "effect target")?;
                    match target_purpose {
                        Some(TargetSelectionPurposeV4::EffectTargets) => json!({"kind": "choose_target",
                            "source": required(view.reference(source, &id_key), "choose_target.source")?,
                            "slot": 0, "target": t, "selected_count": sel, "minimum": min, "maximum": max}),
                        Some(TargetSelectionPurposeV4::LibraryOrder) => {
                            let item = t.get("object").cloned().ok_or("library order over a player")?;
                            json!({"kind": "order_pick", "source": view.reference(source, &id_key), "purpose": "other",
                                   "item": {"object": item}, "position": sel, "count": max})
                        }
                        p => json!({"kind": "select_object", "source": view.reference(source, &id_key),
                            "purpose": if p == Some(TargetSelectionPurposeV4::SearchResult) { "search" } else { "other" },
                            "choice": t, "selected_count": sel, "minimum": min, "maximum": max}),
                    }
                }
                FinishEffectSelection { source, selected_count, .. } => match target_purpose {
                    Some(TargetSelectionPurposeV4::EffectTargets) => json!({"kind": "finish_target_selection",
                        "source": required(view.reference(source, &id_key), "finish_target_selection.source")?,
                        "slot": 0, "selected_count": u32::from(*selected_count)}),
                    p => json!({"kind": "finish_selection", "source": view.reference(source, &id_key),
                        "purpose": if p == Some(TargetSelectionPurposeV4::SearchResult) { "search" } else { "other" },
                        "selected_count": u32::from(*selected_count)}),
                },
                ChooseEffectColor { source, color, .. } => match color_word(*color) {
                    Some(w) => json!({"kind": "choose_color", "source": view.reference(source, &id_key), "purpose": "effect", "color": w}),
                    None => return Err("choose_color offered colorless".into()),
                },
                ChooseEffectNumber { source, number, minimum, maximum, .. } => json!({"kind": "choose_number",
                    "source": view.reference(source, &id_key), "purpose": "amount",
                    "value": number, "minimum": minimum, "maximum": maximum}),
                ChooseEffectBoolean { source, value, .. } => match bool_purpose {
                    Some(BooleanChoicePurposeV4::PayCost) => match view.reference(source, &id_key) {
                        Some(src) => json!({"kind": "optional_cost", "source": src, "cost": "unless_payment", "pay": value}),
                        None => json!({"kind": "choose_boolean", "source": null, "purpose": "other", "value": value}),
                    },
                    Some(BooleanChoicePurposeV4::OptionalEffect) => json!({"kind": "choose_boolean",
                        "source": view.reference(source, &id_key), "purpose": "may_ability", "value": value}),
                    _ => json!({"kind": "choose_boolean", "source": view.reference(source, &id_key), "purpose": "other", "value": value}),
                },
                ChooseOptionalCostUse { use_cost, .. } => match pending_src.as_ref().and_then(|s| view.reference(s, &id_key)) {
                    Some(src) => json!({"kind": "optional_cost", "source": src, "cost": "additional", "pay": use_cost}),
                    None => json!({"kind": "choose_boolean", "source": null, "purpose": "other", "value": use_cost}),
                },
                ChooseOptionalCostWhich { choice, .. } => match pending_src.as_ref().and_then(|s| view.reference(s, &id_key)) {
                    Some(src) => json!({"kind": "choose_cost_option", "source": src, "choice": optional_cost_choice_word(*choice)}),
                    None => json!({"kind": "choose_option", "source": null, "purpose": "other",
                        "option_index": i as u32, "option_count": d.legal_actions.len() as u32,
                        "option_label": optional_cost_choice_word(*choice)}),
                },
                ChooseSpellCopyPayment { source, pay, .. } => json!({"kind": "optional_cost",
                    "source": required(view.reference(source, &id_key), "optional_cost.source")?, "cost": "copy", "pay": pay}),
                ChooseSpellCopyRetarget { source, change_target, .. } => json!({"kind": "choose_boolean",
                    "source": view.reference(source, &id_key), "purpose": "change_copy_targets", "value": change_target}),
                ChooseMadnessCast { card, cast_it, .. } => json!({"kind": "optional_cast",
                    "card": required(view.reference(card, &id_key), "optional_cast.card")?, "method": "madness", "cast_it": cast_it}),
                Discard { cards, .. } => {
                    if cards.len() != 1 {
                        return Err(format!("discard of {} cards in one action", cards.len()));
                    }
                    let card = required(view.reference(&cards[0], &id_key), "discard card")?;
                    let need = remaining_discard.unwrap_or(1).max(1);
                    let max = seq_count + need;
                    match (stage, pending_src.as_ref().and_then(|s| view.reference(s, &id_key))) {
                        (EngineDecisionStageV2::PendingCast | EngineDecisionStageV2::PendingActivation, Some(src)) => json!({
                            "kind": "choose_cost_target", "source": src, "cost_kind": "discard", "candidate": card,
                            "selected_count": seq_count, "minimum": max, "maximum": max}),
                        _ => json!({"kind": "select_object", "source": null, "purpose": "discard",
                            "choice": {"object": card}, "selected_count": seq_count, "minimum": max, "maximum": max}),
                    }
                }
                ChooseAttackerInclusion { attacker, include, .. } => json!({"kind": "declare_attack",
                    "attacker": required(view.reference(attacker, &id_key), "declare_attack.attacker")?,
                    "defender": if *include { json!({"player": opp}) } else { Value::Null }}),
                ChooseBlockerInclusion { attacker, blocker, include, .. } => json!({"kind": "declare_block",
                    "blocker": required(view.reference(blocker, &id_key), "declare_block.blocker")?,
                    "attacker": if *include { required(view.reference(attacker, &id_key), "declare_block.attacker")? } else { Value::Null }}),
                ChooseLegendPermanent { keep, .. } => json!({"kind": "select_object", "source": null, "purpose": "legend_rule",
                    "choice": {"object": required(view.reference(keep, &id_key), "legend keep")?},
                    "selected_count": 0, "minimum": 1, "maximum": 1}),
                OrderTriggers { .. } => return Err("unexpected order_triggers".into()),
                ChooseCombatDamageRange { .. } => return Err("combat damage assignment is engine_order".into()),
                DeclareAttackers { .. } => return Err("aggregate declare_attackers".into()),
                DeclareBlockersForAttacker { .. } => return Err("aggregate declare_blockers".into()),
                Ambiguous { reason } => return Err(format!("ambiguous action: {reason}")),
            };
            cands.push((sem, Answer::Kernel(i)));
        }
        self.assemble(d, seat, group, o, view, cands, priority, seq_tag)
    }

    /// `cast_spell.method` (spec 7.2, 7.4): `null` exactly when the kernel's
    /// next decision for this cast is a `ChooseCastMode`.
    fn cast_method(&self, d: &RlSessionDecisionV1, index: usize, source: &CardStableRefV1) -> Value {
        let def = def_of(source.card_db_id);
        match source.zone {
            Zone::Hand => {
                if def.alt_cost.is_some() || def.omen.is_some() || def.bestow.is_some() {
                    let mut probe = self.session.clone();
                    let a = &d.legal_actions[index];
                    if let Ok(RlSessionResponseV1::Decision(next)) = probe.step(self.episode, d.step, a.selected_index, &a.stable_id) {
                        if next.legal_actions.iter().any(|x| matches!(x.semantic, ActionSemanticV1::ChooseCastMode { .. })) {
                            return Value::Null;
                        }
                    }
                }
                json!("normal")
            }
            Zone::Graveyard => {
                if def.flashback.is_some() {
                    json!("flashback")
                } else if def.escape.is_some() {
                    json!("escape")
                } else {
                    json!("other")
                }
            }
            Zone::Exile => {
                if def.plot_cost.is_some() {
                    json!("plot")
                } else {
                    json!("other")
                }
            }
            _ => json!("other"),
        }
    }
}

fn collect_hidden(v: &Value, viewer: &str, out: &mut Vec<(String, String)>) {
    match v {
        Value::Object(m) => {
            if m.contains_key("object_id") && m.contains_key("zone") && m.contains_key("owner_seat") {
                let zone = m["zone"].as_str().unwrap_or("");
                if zone == "library" || (zone == "hand" && m["owner_seat"].as_str() != Some(viewer)) {
                    out.push((
                        m["card_name"].as_str().map(|s| format!("1{s}")).unwrap_or_default(),
                        m["object_id"].as_str().unwrap_or("").to_string(),
                    ));
                }
                return;
            }
            for (_, x) in m {
                collect_hidden(x, viewer, out);
            }
        }
        Value::Array(a) => {
            for x in a {
                collect_hidden(x, viewer, out);
            }
        }
        _ => {}
    }
}

fn main() {
    for arg in std::env::args().skip(1) {
        match arg.as_str() {
            "--self-test" => {
                let ok = self_test();
                println!("agent_bridge_v2 self-test: {}", if ok { "ok" } else { "FAILED" });
                std::process::exit(if ok { 0 } else { 1 });
            }
            "--x-kernel-flat-v4" => {
                eprintln!("agent_bridge_v2: --x-kernel-flat-v4 is the private flat-encoder contract; not implemented");
                std::process::exit(2);
            }
            _ => {
                eprintln!("usage: agent_bridge_v2 [--self-test]  (unsupported flag {arg:?})");
                std::process::exit(2);
            }
        }
    }
    let mut bridge = Bridge::new();
    let stdin = io::stdin();
    let mut input = stdin.lock();
    let mut stdout = io::stdout().lock();
    let mut buf = Vec::new();
    loop {
        buf.clear();
        match input.read_until(b'\n', &mut buf) {
            Ok(0) => break,
            Ok(_) => {}
            Err(e) => {
                eprintln!("agent_bridge_v2: stdin read failed: {e}");
                std::process::exit(1);
            }
        }
        let mut line: &[u8] = &buf;
        if line.last() == Some(&b'\n') {
            line = &line[..line.len() - 1];
        }
        if line.last() == Some(&b'\r') {
            line = &line[..line.len() - 1];
        }
        let out = bridge.handle(line);
        if writeln!(stdout, "{out}").and_then(|_| stdout.flush()).is_err() {
            std::process::exit(1);
        }
    }
    if bridge.halts > 0 {
        eprintln!("agent_bridge_v2: {} halted game(s)", bridge.halts);
    }
}
