package rules

// resolve_record_flow.go holds the card-flow answer records the resolution
// kernel writes when it serves a converted ask from its tape (W3 step 2,
// group "flow"; resolve_record.go's per-resume-kind switch): each is the
// event-emitting half of a legacy resume arm, shared with that arm.

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// resumeKindDredge is drawFor's Dredge ask (effects/cardflow.go).
const resumeKindDredge = "dredge"

// dredgeAnswerApply applies an answered Dredge ask for player p: the chosen
// graveyard card's replacement (CR 702.55: mill N, return it to hand), or,
// for the "Draw card" option or an empty answer, the ordinary draw the ask
// held back. The one home of the "dredge" answer, shared by the resume arm
// and the kernel's answer record; the asking walk then continues past the
// draw's cursor.
func dredgeAnswerApply(e *Engine, p state.PlayerID, chosen []decision.Option) {
	if len(chosen) > 0 && chosen[0].Kind == "dredge" {
		e.applyDredge(p, chosen[0].Obj)
		return
	}
	e.resumeOrdinaryDraw(p)
}
