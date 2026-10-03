package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFinalityOnlyExilesCreatureDeaths(t *testing.T) {
	t.Parallel()
	creature := "Name:Finality Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	artifact := "Name:Finality Rock\nTypes:Artifact\nOracle:x\n"
	e, _, bear := newFixtureDeck(t, 91201, creature, artifact)
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "FINALITY", Amount: 1})
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("precondition: bear = %+v", o)
	}
	e.emit(events.Event{Kind: events.Damage, Obj: bear, Amount: 2})
	e.checkStateBased()
	if got := e.G.Obj(bear).Zone; got != state.ZExile {
		t.Fatalf("finality creature zone = %s, want exile", got)
	}

	var rock state.ObjID
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if e.G.Obj(id).Face().Name == "Finality Rock" {
			rock = id
			break
		}
	}
	if rock == 0 {
		t.Fatal("precondition: artifact not found")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: rock, From: state.ZLibrary, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: rock, Counter: "FINALITY", Amount: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: rock, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(rock).Zone; got != state.ZGraveyard {
		t.Fatalf("finality noncreature zone = %s, want graveyard", got)
	}
}
