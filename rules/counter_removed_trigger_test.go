package rules

// trig:CounterRemoved (Mode$ CounterRemoved, "whenever a counter is removed
// from ~" / "when the last <kind> counter is removed from ~"), pinned on real
// corpus cards: Protean Hydra (battlefield scope, every removal), Orcish Mine
// (NewCounterAmount$ 0 -- the LAST-ore-counter gate), Watcher of Hours (exile
// scope + ValidPlayer$ You). The triggering event is a CounterChange with a
// negative Amount, the mirror of CounterAdded's positive one, emitted the way
// the activate/alternative_costs tests drive a CounterChange directly.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCounterRemovedIsRegistered: the RegisterNonAPI registration makes
// effects.Supported() report the primitive, so the coverage report/ratchet
// see it.
func TestCounterRemovedIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["trig:CounterRemoved"] {
		t.Fatal("effects.Supported() lacks trig:CounterRemoved")
	}
}

// TestCounterRemovedProteanHydraFiresPerRemovalOnBattlefield: the Hydra loses
// a +1/+1 counter and its trigger queues once; resolving it registers the
// end-step delayed trigger that puts two +1/+1 counters back, remembering the
// Hydra (RememberObjects$ TriggeredCardLKICopy).
func TestCounterRemovedProteanHydraFiresPerRemovalOnBattlefield(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, mshCorpusCard(t, "Protean Hydra"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 3})
	if e.G.Obj(id).Counter("P1P1") != 3 {
		t.Fatalf("setup: hydra counters = %d, want 3", e.G.Obj(id).Counter("P1P1"))
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: -1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after one removal = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if len(e.G.Delayed) != 1 {
		t.Fatalf("delayed registrations after resolution = %d, want 1", len(e.G.Delayed))
	}
	dt := e.G.Delayed[0]
	if dt.Source != id || dt.Execute != "DBPutCounters" {
		t.Fatalf("delayed trigger = %+v, want source %d executing DBPutCounters", dt, id)
	}
	if len(dt.Remembered) != 1 || dt.Remembered[0].Obj != id {
		t.Fatalf("delayed trigger remembered %+v, want the hydra %d", dt.Remembered, id)
	}
}

// TestCounterRemovedIgnoresCounterAdd: a POSITIVE P1P1 CounterChange on the
// same card must not queue the removal trigger (CounterAdded's exact mirror
// condition).
func TestCounterRemovedIgnoresCounterAdd(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, mshCorpusCard(t, "Protean Hydra"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a counter ADD queued the removal trigger: %d pending", len(e.pendingTriggers))
	}
}

// TestCounterRemovedIgnoresOtherCounterKind: CounterType$ filters -- a P1P1
// removal on the Mine (ORE-only trigger) queues nothing.
func TestCounterRemovedIgnoresOtherCounterKind(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, mshCorpusCard(t, "Orcish Mine"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: -1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a P1P1 removal fired the ORE-only trigger: %d pending", len(e.pendingTriggers))
	}
}

// TestCounterRemovedNewCounterAmountZeroFiresOnlyOnTheLast: Orcish Mine's
// "when the LAST ore counter is removed" -- removals that leave ore counters
// in place queue nothing; the removal that takes the total to 0 queues
// exactly one.
func TestCounterRemovedNewCounterAmountZeroFiresOnlyOnTheLast(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, mshCorpusCard(t, "Orcish Mine"))
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "ORE", Amount: 3})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "ORE", Amount: -1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a removal leaving 2 ore counters fired the last-counter trigger: %d pending", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "ORE", Amount: -1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a removal leaving 1 ore counter fired the last-counter trigger: %d pending", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "ORE", Amount: -1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("the last removal queued %d triggers, want 1", len(e.pendingTriggers))
	}
	// A batch that removes the whole remainder in one event crosses the gate
	// too: the post-event total is the gate.
	e2 := combatEngine(t)
	id2 := onBoardCard(t, e2, 0, mshCorpusCard(t, "Orcish Mine"))
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "ORE", Amount: 3})
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "ORE", Amount: -3})
	if len(e2.pendingTriggers) != 1 {
		t.Fatalf("a -3 batch on 3 counters queued %d triggers, want 1", len(e2.pendingTriggers))
	}
}

// TestCounterRemovedExileScopedCarrier: Watcher of Hours' trigger is scoped
// TriggerZones$ Exile -- a TIME removal while exiled fires (and resolving it
// runs the Surveil body's ask-free path), while the same card on the
// battlefield with its trigger's zone clause unsatisfied does not.
func TestCounterRemovedExileScopedCarrier(t *testing.T) {
	t.Parallel()
	// In exile, with TIME counters: the removal fires.
	e := combatEngine(t)
	o := e.G.AddObject(mshCorpusCard(t, "Watcher of Hours"), 0)
	id := o.ID
	o.Zone = state.ZExile
	e.G.SetZone(state.ZExile, 0, append(e.G.Zone(state.ZExile, 0), id))
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: 6})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "TIME", Amount: -1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("exiled removal queued %d triggers, want 1", len(e.pendingTriggers))
	}

	// Zone gate: the same card on the battlefield does not fire.
	e2 := combatEngine(t)
	id2 := onBoardCard(t, e2, 0, mshCorpusCard(t, "Watcher of Hours"))
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "TIME", Amount: 6})
	e2.emit(events.Event{Kind: events.CounterChange, Obj: id2, Counter: "TIME", Amount: -1})
	if len(e2.pendingTriggers) != 0 {
		t.Fatalf("battlefield removal on an exile-scoped carrier queued %d triggers, want 0", len(e2.pendingTriggers))
	}

	// The exile-scoped firing is also not a CounterAdded mirror-failure: a
	// positive TIME put on the exiled card queues nothing.
	e3 := combatEngine(t)
	o3 := e.G.AddObject(mshCorpusCard(t, "Watcher of Hours"), 0)
	id3 := o3.ID
	o3.Zone = state.ZExile
	e3.G.SetZone(state.ZExile, 0, append(e3.G.Zone(state.ZExile, 0), id3))
	e3.emit(events.Event{Kind: events.CounterChange, Obj: id3, Counter: "TIME", Amount: 1})
	if len(e3.pendingTriggers) != 0 {
		t.Fatalf("a TIME add on the exiled carrier queued %d triggers, want 0", len(e3.pendingTriggers))
	}
}
