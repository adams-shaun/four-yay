package rules

// resolve_land_mayask.go is the ask-free predicate of a land play (the tape
// run a play_land intent begins): the land's entry is the one place the play
// can pose a decision (an as-enters choice, a shock land's pay-life
// election, Hideaway, a CR 616.1 order choice among entry replacements).

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// tapeLandMayAsk reports whether playing land obj may pose a decision, judged
// from the face's entry text and the replacements on the board. true is
// always safe.
func tapeLandMayAsk(e *Engine, obj state.ObjID) bool {
	o := e.G.Obj(obj)
	if o == nil {
		return true
	}
	f := o.Face()
	if f == nil || cards.FaceEntryMayAsk(f) || len(f.Repls) > 0 {
		return true
	}
	return tapeReplMayAsk(e, "Moved")
}

// tapeStepMayAsk reports whether the pass that ends the current step (empty
// stack) begins a turn-based action that may pose a decision: the draw step's
// draw, which a Dredge card in a graveyard replaces with its election.
func tapeStepMayAsk(e *Engine) bool {
	return e.G.Step == state.StepUpkeep && tapeDredgeMayAsk(e)
}
