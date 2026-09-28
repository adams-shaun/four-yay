// Package v2engine serves the ENVIRONMENT role of SpellBench protocol v2
// ("spellbench/v2", spec/SPELLBENCH_PROTOCOL_V2.md in the spellbench
// repository) over the real gorge rules engine: hello, reset, step and
// validate_deck as NDJSON, one game per process at a time (spec 2, 9).
//
// It exists to run SpellBench agents "in reverse" on gorge: the reference
// python host drives this server exactly as it drives any v2 engine, so our
// Go agent (cmd/sbagent) and the python builtins can play full games on real
// gorge rules, and the host's live validator (V1-V10) checks every decision
// this package emits. It is deliberately not Jack Maiorino's gorge adapter
// (engines/gorge on the spellbench repo's gorge-adapter branches), which is
// a separate, much larger conformance project still mid-way (tasks 1-14 of
// 30, no game loop or server yet); see the design spec's "v2 on gorge
// (reverse adapter)" section for why and for the gaps observed.
//
// # Mapping (every choice made here)
//
// Observation (spec 6) is built directly from gorge state for the viewer,
// never from an omniscient read: the other seat's hand is a count, libraries
// are counts, face-down objects the viewer does not control are nameless.
// Object ids follow spec 5.3's recommended construction (HMAC of the game
// secret, the viewer and "<gorge ObjID>:z<zone changes>"; a card shown from
// a hidden zone gets ":look:<n>" with n advanced per gorge decision that
// shows it). gorge reuses an ObjID across zones, so zone changes are counted
// from the event log (MoveZone, Draw, PutOnStack). Optional observation
// flags: keywords and full_name are declared ("A // B" for a multi-face
// card, which is also how the catalog decklists name it, spec 4.4);
// known_cards is false, so known lists only the cards shown in the current
// decision (searching, looked_at, revealed). A stack ability is named after
// its source; a source that moved to a hidden zone keeps the name the viewer
// already saw on that stack object, and one activated from a hand is named
// (activation reveals it, CR 602.2a). gorge's option labels are sent as
// display_text only when they name nothing the viewer may not see.
//
// Decisions (spec 7, 8): gorge asks one Decision with an option list and a
// Min..Max answer; this package decomposes it into v2 wire decisions and
// submits the assembled Intent once the last substep is answered:
//
//   - KPriority -> one priority decision: pass first, then play_land,
//     cast_spell (method from the option's mode; a kicked, buyback,
//     entwined... twin of a plain cast is the plain cast_spell followed by
//     an optional_cost decision), activate_mana_ability
//     (every mana ability is a candidate: mana_payment is not auto-paid in
//     the default "manual" surface), activate_ability, special_action.
//     concede is never offered (the host owns concession). Under -mana
//     autopay the mana abilities are hidden and planner-paid casts
//     (gorge's PaymentActions) are offered instead (engine_autopay).
//   - KAttackers / KBlockers -> declare_attack / declare_block groups, one
//     substep per creature, candidates filtered so every one completes to
//     an answer gorge's validator accepts (no dead ends, spec 7.1).
//   - KTarget -> choose_target (fixed-count group) or choose_target plus
//     finish_target_selection (variable count, one group per decision).
//   - KChoose object picks (search, dig, discard, sacrifice, tap/return
//     costs, untap, hand_move, keep) -> select_object / choose_cost_target
//     groups; library cards appear in known with fresh ids.
//   - KChoose value picks -> choose_number (x), choose_color (mana, color),
//     choose_boolean (yes/no), choose_name (type) where the value maps onto
//     the spec vocabulary; everything else single-pick -> choose_option.
//   - KModes -> choose_spell_mode, or optional_cost unless_payment.
//   - KTriggerOrder -> order_pick triggers (n-1 posed picks).
//   - KTriggerOptional -> choose_boolean optional_trigger.
//   - KArrange -> the 2n-1 arrangement group (arrange_card, order_pick).
//   - Anything that fits none of the above, or whose candidates would not
//     be pairwise distinct, is posed as one enumerated choose_option over
//     every complete answer gorge's validator accepts (spec 7.1's "one
//     enumerated decision"); these are counted.
//
// mulligan is "none" and the starting player "host_assigned" (gorge's toss
// choice is answered for the named seat), so neither is ever posed.
//
// Test mode: Options.Truth receives one JSON line per posed decision with
// the engine truth for the acting seat (gorge's own seat projection, the
// exact library and opponent-hand contents). It is a side channel for the
// shadow-state check only; nothing on it ever reaches an agent.
package v2engine
