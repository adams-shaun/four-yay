package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// One surviving-to-the-end witness does not certify simultaneous deaths.
// Every real Blood Artist present before this batch must witness both deaths.
func TestCR704EveryDyingBloodArtistWitnessesWholeBatch(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Blood Artist", "Blood Artist"}, nil)
	a := crAbortMove(t, e, 0, "Blood Artist", state.ZBattlefield)
	b := crAbortMove(t, e, 0, "Blood Artist", state.ZBattlefield)
	if a == b {
		t.Fatal("CR 704.3: fixture did not supply two distinct witnesses")
	}
	for _, id := range []state.ObjID{a, b} {
		tr := e.G.Obj(id).Face().Triggers
		if len(tr) != 1 || tr[0].Mode != "ChangesZone" || tr[0].Params["ValidCard"] != "Card.Self,Creature.Other" {
			t.Fatal("CR 704.3: real Blood Artist witness fixture changed")
		}
		e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 1})
	}
	before := len(e.pendingTriggers)
	e.checkStateBased()
	for _, id := range []state.ObjID{a, b} {
		if e.G.Obj(id).Zone != state.ZGraveyard {
			t.Fatalf("CR 704.3: witness %d never died", id)
		}
		n := 0
		for _, tr := range e.pendingTriggers[before:] {
			if tr.Source == id {
				n++
			}
		}
		if n != 2 {
			t.Errorf("CR 704.3/603.10a: dying Blood Artist %d witnessed %d of two simultaneous deaths", id, n)
		}
	}
}
