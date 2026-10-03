package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFinalityDestroyRespectsDerivedNonCreatureType(t *testing.T) {
	t.Parallel()
	e, cfg, bear := newFixtureDeck(t, 91203, "Name:Finality Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "FINALITY", Amount: 1})
	e.AddContinuous(state.ContinuousEffect{Source: bear, Controller: 0, Affects: "Card.Self", Layer: state.LType,
		RemoveCardTypes: true})
	for _, typ := range e.Derived(bear).Types {
		if typ == "Creature" {
			t.Fatalf("precondition: derived types still include Creature: %v", e.Derived(bear).Types)
		}
	}
	if o := e.G.Obj(bear); o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("precondition: finality bear = %+v", o)
	}

	effects.Resolve(e, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Destroy", Params: map[string]string{}})
	if got := e.G.Obj(bear).Zone; got != state.ZGraveyard {
		t.Fatalf("derived noncreature with finality went to %s, want graveyard", got)
	}
	replayCheck(t, e, cfg)
}
