package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A creature selected for both parts must receive its Blight counters before
// the Sacrifice moves it off the battlefield. This directly exercises the
// triggered-cost settlement order with the same object reserved for each part.
func TestTriggeredCostBlightAndSacrificeSameCreature(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Blighted Blackthorn", "Grizzly Bears")
	source := searchMoveByName(t, e, "Blighted Blackthorn", state.ZBattlefield)
	creature := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source %d is not on the battlefield: %v", source, o)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield || !o.EffectiveIsCreature() {
		t.Fatalf("precondition: shared Blight/Sac creature %d is not a battlefield creature: %v", creature, o)
	}
	if got := len(e.G.Zone(state.ZBattlefield, 0)); got < 2 {
		t.Fatalf("precondition: expected source and creature on battlefield, found %d permanents", got)
	}

	amount := ParseCost("Blight<2> Sac<1/Creature.YouCtrl>")
	if len(amount.Blight) != 1 || amount.Blight[0].N != 2 || len(amount.Sac) != 1 {
		t.Fatalf("precondition: combined cost did not parse as Blight<2> and Sac<1>: %+v", amount)
	}
	mark := len(e.L.Events)
	e.settleTriggeredMandatory(&triggeredEffectCost{
		resume:  &resumePoint{}, // no stack object: settlement returns after paying.
		source:  source,
		player:  0,
		amount:  amount,
		sacs:    []state.ObjID{creature},
		blights: []state.ObjID{creature},
	})

	counterAt, sacrificeAt := -1, -1
	for i, event := range e.L.Events[mark:] {
		if event.Kind == events.CounterChange && event.Obj == creature && event.Counter == "M1M1" && event.Amount == 2 {
			counterAt = i
		}
		if events.IsSacrifice(event) && event.Obj == creature {
			sacrificeAt = i
		}
	}
	if counterAt < 0 {
		t.Fatal("settlement did not emit CounterChange(M1M1,+2) for the shared creature")
	}
	if sacrificeAt < 0 {
		t.Fatal("settlement did not sacrifice the shared creature")
	}
	if counterAt >= sacrificeAt {
		t.Fatalf("Blight counter event index %d must precede sacrifice event index %d", counterAt, sacrificeAt)
	}
	if o := e.G.Obj(creature); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("shared creature zone = %v, want graveyard after paying sacrifice", o)
	}
}
