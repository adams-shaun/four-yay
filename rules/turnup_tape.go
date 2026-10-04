package rules

import "github.com/adams-shaun/gorge/decision"

// tapeTurnUpStarts reports whether answering d with in may settle a
// morph-family turn-face-up special action (CR 708.6 / CR 116.2b) whose
// TurnFaceUp event a replacement that may ask intercepts -- Gift of Doom's
// "As this is turned face up, you may attach it to a creature" (an Optional$
// ReplaceWith$ DBAttach pick). A special action uses no stack, so the
// replacement's ask runs inside the Submit that settles the action: the
// priority answer that takes the action, or the answer to one of its cost
// asks. That Submit is then a tape run, so the ask is posed and answered
// from the tape instead of taking its no-run default.
func tapeTurnUpStarts(e *Engine, d *decision.Decision, in decision.Intent) bool {
	switch {
	case d.Kind == decision.KChoose && e.choosing == chooseTurnUp && e.turnUp != nil:
	case d.Kind == decision.KPriority && len(in.Choices) > 0 && firstChosen(d, in).Kind == "turn_face_up":
	default:
		return false
	}
	return tapeReplMayAsk(e, "TurnFaceUp") || tapeAnyReplBodyMayAsk(e)
}
