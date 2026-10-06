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
	if live := src.Face(); live != nil && idx < len(live.Triggers) && live.Triggers[idx].Effect == pt.SA {
		return int32(idx)
	}
	for fi, f := range src.Card.Faces {
		if f != nil && fi != int(src.FaceIdx) && idx < len(f.Triggers) && f.Triggers[idx].Effect == pt.SA {
			return events.TriggerPushFaceAmount(fi, idx)
		}
	}
	return int32(idx)
}

// inactiveFaceTrigger finds the printed trigger whose compiled body is sa on
// the card face that is NOT o's live face (see inactiveFaceLayout).
func inactiveFaceTrigger(o *state.Object, sa *cards.SA) (cards.Trigger, *cards.Face, bool) {
	if !inactiveFaceLayout(o.Card) {
		return cards.Trigger{}, nil, false
	}
	for fi, f := range o.Card.Faces {
		if f == nil || fi == int(o.FaceIdx) {
			continue
		}
		for _, t := range f.Triggers {
			if t.Effect == sa {
				return t, f, true
			}
		}
	}
	return cards.Trigger{}, nil, false
}
