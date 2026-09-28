// Package v1agent serves the SpellBench protocol v1 AGENT role
// (spec/SPELLBENCH_PROTOCOL_V1.md, Section 10) so any Go policy can play on
// a v1 environment -- in particular mtg-kernel, the engine the public
// pauper-kernel leaderboard is rated on.
//
// # Layers
//
//   - Agent (agent.go): NDJSON framing, the hello / game_start / choose /
//     game_over state machine, the single-entry idempotent retry cache and
//     the closed agent error-code set. A policy failure (panic, out-of-range
//     answer) never reaches the wire as an error -- that would forfeit the
//     game -- it is logged, counted, and answered by the fallback
//     (heuristic) choice instead.
//   - Decision (decision.go): the decoded v1 decision. Candidates keep
//     their raw semantic bytes so the semantic_echo is the engine's own
//     object, field for field.
//   - Kernel view (kernel.go): the optional x_kernel_v5 extension that the
//     mtg-kernel bridge attaches to every decision (spec Section 9): the
//     acting seat's ObservationV5 (both battlefields with power, toughness,
//     keywords, tapped/sick state, damage; graveyards; stack; combat; its
//     own hand) and the kernel legal actions in candidate order, which
//     carry engine-stable arena ids for every referenced object. It is
//     observer-relative -- exactly what the seat may see -- and licensed by
//     the protocol for kernel-native bots.
//   - Board (board.go): the policy-facing model. Built from the kernel view
//     when present; otherwise reconstructed from what v1 alone carries
//     (state_summary counts and the card names candidates reference).
//   - Card facts (cardfacts.go): per-card knowledge for the pool, generated
//     from gorge's compiled card IR (damage amounts, target polarity, types,
//     keywords) plus hand-written roles for cards whose play pattern the IR
//     does not express.
//   - Policies (builtins.go, tactical.go): Policy is the plug point. The
//     three SpellBench builtins are ported choice-for-choice (uniform's
//     SplitMix64 stream included); Tactical is the board-reading policy.
package v1agent
