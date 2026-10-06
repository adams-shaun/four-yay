package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A transforming or modal double-faced permanent that leaves the battlefield
// is front face up in its next zone (CR 712.8a / 712.4d, folded in
// events.Apply's Move), but a "when this dies" line printed on its BACK face
// triggered from the look-back board, before that reset. The ability must
// still be that back-face line: the push names the face it came from and the
// owning-line scan finds it there. Split/Room layouts are excluded: their
// alternate-face triggers already travel by name (checkFaceTriggers' delayed
// path) and are not an inactive-face leftover.
func inactiveFaceLayout(c *cards.Card) bool {
	return c.BackFaceIsBattlefieldOnly()
}

// triggerPushAmount is the TriggerPush Amount for pt: the plain trigger index
// when the line is on the source's live face (every ordinary push), the
// face-packed value (events.TriggerPushFaceAmount) when the compiled line pt.SA
// is on the card's other face.
func triggerPushAmount(g *state.Game, pt pendingTrigger) int32 {
	idx := pt.Idx
	src := g.Obj(pt.Source)
	if src == nil || !inactiveFaceLayout(src.Card) || pt.SA == nil || idx < 0 {
		return int32(idx)
	}
	_, _, face, faceIdx, ok := printedFaceTrigger(src, pt.SA)
	if ok {
		if face == int(src.FaceIdx) {
			return int32(faceIdx)
		}
		return events.TriggerPushFaceAmount(face, faceIdx)
	}
	return int32(idx)
}

// printedFaceTrigger finds the owning face and trigger index for a compiled
// printed trigger body. Both event encoding and resolution-time lookup use
// this one live-face-first ownership rule.
func printedFaceTrigger(o *state.Object, sa *cards.SA) (cards.Trigger, *cards.Face, int, int, bool) {
	if o == nil || sa == nil || o.Card == nil {
		return cards.Trigger{}, nil, 0, 0, false
	}
	if live := o.Face(); live != nil {
		for ti, t := range live.Triggers {
			if t.Effect == sa {
				return t, live, int(o.FaceIdx), ti, true
			}
		}
	}
	if !inactiveFaceLayout(o.Card) {
		return cards.Trigger{}, nil, 0, 0, false
	}
	for fi, f := range o.Card.Faces {
		if f == nil || fi == int(o.FaceIdx) {
			continue
		}
		for ti, t := range f.Triggers {
			if t.Effect == sa {
				return t, f, fi, ti, true
			}
		}
	}
	return cards.Trigger{}, nil, 0, 0, false
}
