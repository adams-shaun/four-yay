package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The round-2 review's class fix (Gitaxian Probe's Look$ leak): every
// looker-scoped effect records its private look through effects' emitLook —
// one Secret Note per looker, Player = that looker. This file pins that
// contract across ALL of them, table-driven, so the next look-shaped effect
// that bypasses the helper fails here.

// lookBoard builds a 2-seat game; seat 1's hand holds a creature, a land and
// an instant (in that order); seat 0's library holds one land.
func lookBoard(t *testing.T) (*askHost, []state.ObjID, state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	hand := []state.ObjID{
		h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1).ID,
		h.g.AddObject(mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"), 1).ID,
		h.g.AddObject(mkCard(t, "Name:Bolt\nTypes:Instant\nOracle:x\n"), 1).ID,
	}
	for _, id := range hand {
		h.g.Obj(id).Zone = state.ZHand
	}
	h.g.SetZone(state.ZHand, 1, hand)
	mtn := h.g.AddObject(mkCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"), 0).ID
	mtn2 := h.g.AddObject(mkCard(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"), 0).ID
	bear0 := h.g.AddObject(mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0).ID
	for _, id := range []state.ObjID{mtn, mtn2, bear0} {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{mtn, mtn2, bear0})
	return h, hand, mtn
}

// secretLookNotes returns the Secret look Notes (emitLook's shape).
func secretLookNotes(log []events.Event) []events.Event {
	var out []events.Event
	for _, e := range log {
		if e.Kind == events.Note && e.Secret {
			out = append(out, e)
		}
	}
	return out
}
