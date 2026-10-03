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
	if f == nil || cards.FaceEntryMayAsk(f) {
		return true
	}
	if o.Card != nil {
		// A modal double-faced land may be played as its other face.
		for _, cf := range o.Card.Faces {
			if cf != nil && cards.FaceEntryMayAsk(cf) {
				return true
			}
		}
	}
	return tapeReplMayAsk(e, "Moved")
}

// tapeStepMayAsk reports whether the pass that ends the current step (empty
// stack) begins a turn-based action that may pose a decision: the draw step's
// draw, which a Dredge card in a graveyard replaces with its election.
func tapeStepMayAsk(e *Engine) bool {
	switch e.G.Step {
	case state.StepUpkeep:
		return tapeDredgeMayAsk(e)
	case state.StepDeclareBlockers, state.StepCombatDamage:
		// The pass into the combat damage step: a DamageDone replacement
		// that elects asks as the damage is dealt.
		return tapeReplMayAsk(e, "DamageDone") || tapeAnyReplBodyMayAsk(e)
	}
	return false
}

// tapeAnyReplBodyMayAsk reports whether any replacement source on the board
// (or in a replacement-bearing zone) carries a ReplaceWith$ body that may
// ask, or an Optional$ election, for any event: the broad board gate of a
// turn-based action whose emissions can run any replacement (combat damage
// and the deaths it causes).
func tapeAnyReplBodyMayAsk(e *Engine) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		if ce := &ceL[ceI]; ce.ReplacementEvent != "" && cards.ReplParamsMayElect(ce.ReplacementParams) {
			return true
		}
	}
	ask := false
	e.forEachReplacementSourceFor(^uint32(0), func(id state.ObjID) {
		o := e.G.Obj(id)
		if ask || o == nil {
			return
		}
		f := o.Face()
		if f == nil {
			return
		}
		for i := range f.Repls {
			r := &f.Repls[i]
			if r.Event == "Moved" {
				continue // entries are asked where they happen (the land play, a resolution)
			}
			if cards.ReplMayElect(r) || (r.With != nil && cards.SAChainMayAsk(r.With, f.SVars, nil, false)) {
				ask = true
				return
			}
		}
	})
	return ask
}
