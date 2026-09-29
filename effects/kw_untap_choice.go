// RegisterNonAPI for the corpus's printed bare keyword line
// "You may choose not to untap CARDNAME during your untap step." (CR 502.2).
// The line carries no colon, so KeywordHead keeps the WHOLE sentence as the
// head and cards.Face.Primitives interns `kw:<sentence>` — which without this
// registration reads as a phantom unsupported primitive even though the
// behaviour is implemented: rules/untap.go's hasUntapStepChoice recognises the
// sentence verbatim on every carrier's face and rules/turn.go's untap scan
// poses the controller the untap/keep-tapped choose decision (option 0 untap,
// option 1 keep tapped) for each such permanent at its controller's untap
// step.
//
// This is the exact-sentence registration pattern cards/kw_prevent.go's doc
// comment names: the sentence IS the engine's head, so the supported set is
// keyed on the sentence itself. A corpus spelling that drifts fails closed —
// the head lookup misses, the phantom reappears in the coverage report, and
// the ratchet named in rules/untap_choice_head_test.go fails on the drift.
//
// Measured corpus carriers at the 2026-09-28 pin: 45 card files
// (`grep -rln 'choose not to untap' .cards/cardsfolder | wc -l`).
package effects

func init() {
	RegisterNonAPI("kw:You may choose not to untap CARDNAME during your untap step.")
}
