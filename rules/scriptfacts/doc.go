// Package scriptfacts holds the pure facts a card's PRINTED text and compiled
// Forge script imply, derived without evaluating any game state: the printed
// keyword-head summary the priority walk reads off a hand card, the foretell
// alternative cost, a trigger's Redis 603.5 optionality, the number of turns an
// ability's SkipTurn rider takes, and the triggered ability a granted keyword
// line mints.
//
// It is a leaf of the rules-engine lasagna (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §3, and the
// 2026-10-06 rules-split plan). It holds no *rules.Engine and reads no game
// state: every function is a pure function of a cards.Face, a cards.SA or a
// cards.Trigger, so a moving game cannot change an answer and a replay cannot
// diverge. It imports only cards, state and rules/cost (the L2 cost
// vocabulary leaf); package rules reaches it through one bridge file,
// rules/scriptfacts_bridge.go, which aliases the types and forwards the
// functions under their historical rules names, so the move left rules' call
// sites untouched.
//
// internal/archtest TestScriptFactsImportsStayBelowRules pins this package's
// import set.
package scriptfacts
