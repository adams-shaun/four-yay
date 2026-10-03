// Package chars is the read-only characteristic computation of CR 613: the
// layer walk that turns an object's printed face and the active continuous
// effects into its current characteristics (effects.Chars). It holds the
// layer-4 type walk (Types), the layer-3/5/6 walk and its CR 613.6
// dependency order (Compute), the layer-7 P/T walk (PT), the layer-3 name
// shortcut (Name), the base keyword list, the type vocabulary and the
// layer-4 type transforms, and the layer-7a characteristic-defining P/T
// evaluators (CDASetPT, CDAPTStatic).
//
// It is an L4 package of the rules-engine lasagna (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §3, W5
// step E4). It never holds a *rules.Engine: the walk reads the game through
// Board, a narrow read-only interface package rules implements
// (rules/chars_board.go), and keeps its per-call buffers in a Scratch the
// Engine embeds. Nothing here emits an event or writes game state.
//
// What stays in package rules, behind Board: the effect lifecycle
// (AddContinuous, expiry, the static scan that builds the active list) and
// its caches, the walk-scoped Derived memo and the printed fast paths, the
// incremental layer-4/5/6 tables, and the Affected$ applicability match
// (Board.Matches), whose spec context carries engine-owned tables, the
// cast-provenance gate and a resolver closure that must stay on the
// engine's stack.
//
// internal/archtest TestCharsImportsStayBelowRules pins this package's
// import set (it never imports rules or an L5 subsystem package), and
// TestCharsBoardOnlyShrinks ratchets Board's method count.
package chars
