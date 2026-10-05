package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Class level is a designation, not a LEVEL counter (CR 716.2b, 716.4).
func TestClassLevelDesignationIsNotACounter(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Artist's Talent")
	id := classMove(t, e, "Artist's Talent", state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Class must be on the battlefield")
	}
	if got := o.ClassLevel(); got != 1 {
		t.Fatalf("entry level = %d, want 1", got)
	}
	if got := o.Counter("LEVEL"); got != 0 {
		t.Fatalf("entry has %d LEVEL counters", got)
	}
	addMana(t, e, 0, "RRRRRR")
	classLevelUp(t, e, id, 0)
	if got := o.ClassLevel(); got != 2 {
		t.Fatalf("level after activation = %d, want 2", got)
	}
	if got := o.Counter("LEVEL"); got != 0 {
		t.Fatalf("level-up placed %d LEVEL counters", got)
	}
	changed := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ClassLevelChange && ev.Obj == id {
			changed = true
		}
		if ev.Kind == events.CounterChange && ev.Obj == id && ev.Counter == "LEVEL" {
			t.Fatal("level-up emitted a LEVEL counter change (visible to proliferate)")
		}
	}
	if !changed {
		t.Fatal("level-up did not emit a replay-visible designation change")
	}
	// Even a separately placed LEVEL counter cannot become a Class level.
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LEVEL", Amount: 2})
	if got := o.Counter("LEVEL"); got != 2 {
		t.Fatalf("precondition: counter placement = %d, want 2", got)
	}
	if got := o.ClassLevel(); got != 2 {
		t.Fatalf("unrelated counter changed Class level to %d", got)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "LEVEL", Amount: -2})
	if got := o.ClassLevel(); got != 2 {
		t.Fatalf("counter removal changed Class level to %d", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if got := o.ClassLevel(); got != 1 {
		t.Fatalf("new object retained level %d, want 1", got)
	}
}
