package rules

// tapeTurnUpReplMayAsk reports whether answering a turn-up may settle a
// morph-family turn-face-up special action (CR 708.6 / CR 116.2b) whose
// TurnFaceUp event a replacement that may ask intercepts -- Gift of Doom's
// "As this is turned face up, you may attach it to a creature" (an Optional$
// ReplaceWith$ DBAttach pick). A special action uses no stack, so the
// replacement's ask runs inside the Submit that settles the action: the
// priority answer that takes the action, or the answer to one of its cost
// asks. That Submit is then a tape run, so the ask is posed and answered
// from the tape instead of taking its no-run default.
//
// StartsResolution reads the two shapes itself (a KChoose answer while
// e.choosing == chooseTurnUp, a priority answer whose first choice is
// optTurnFaceUp); this is the board half.
func tapeTurnUpReplMayAsk(e *Engine) bool {
	return tapeReplMayAsk(e, "TurnFaceUp") || tapeAnyReplBodyMayAsk(e)
}
