package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// handCard adds a card named name to owner's hand and returns its id.
func handCard(t *testing.T, h *fakeHost, name string, owner state.PlayerID) state.ObjID {
	t.Helper()
	o := h.g.AddObject(mkCard(t, "Name:"+name+"\nTypes:Creature\nPT:1/1\nOracle:x\n"), owner)
	o.Zone = state.ZHand
	return o.ID
}

// revealPickPublicNotes returns every non-Secret Note carrying ids, in order.
func revealPickPublicNotes(log []events.Event) []events.Event {
	var out []events.Event
	for _, e := range log {
		if e.Kind == events.Note && !e.Secret && len(e.IDs) > 0 {
			out = append(out, e)
		}
	}
	return out
}
