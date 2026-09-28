package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestCountersRemainPreservesCountersExceptHandAndLibrary(t *testing.T) {
	t.Parallel()
	card := mshCorpusCard(t, "Me, the Immortal")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, card)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Me, the Immortal is not on the battlefield")
	}
	if len(o.Face().Statics) == 0 || !e.countersRemainApplies(id) {
		t.Fatal("precondition: Me, the Immortal's CountersRemain static did not match")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition: added P1P1 counters = %d, want 2", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZExile})
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("P1P1 counters after battlefield-to-exile move = %d, want 2", got)
	}
	if len(e.L.Events) == 0 || e.L.Events[len(e.L.Events)-1].Counter != events.MarkCountersRemainMove("") {
		t.Fatal("MoveZone event did not record the CountersRemain marker")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZBattlefield})
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("recast zone = %s, want battlefield", e.G.Obj(id).Zone)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("P1P1 counters after exile recast = %d, want 2", got)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
	if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
		t.Fatalf("P1P1 counters after move to hand = %d, want 0", got)
	}
}
