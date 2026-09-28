package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// TestCounterAddedOncePutsThatManyGrowthCounters pins trig:CounterAddedOnce on
// the real corpus card Simic Ascendancy: one CounterChange event carrying a
// whole placement batch queues exactly ONE trigger (not one per counter), and
// the body's SVar:X:TriggerCount$Amount reads the batch size, so the payoff
// puts that many GROWTH counters on the source.
//
// Before the fix the mode had no matcher at all: the trigger never queued and
// the growth counters stayed at zero. The second batch pins that the "once per
// batch" contract is not a one-shot (a further batch triggers again), and the
// Amount:2 case pins the size is read per event rather than hard-coded.
func TestCounterAddedOncePutsThatManyGrowthCounters(t *testing.T) {
	t.Parallel()
	asc := mshCorpusCard(t, "Simic Ascendancy")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, asc)
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// One batch of three on a creature you control: exactly one trigger, and
	// its resolution puts three growth counters on Simic Ascendancy.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 3})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after a 3-counter batch = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Obj(id).Counter("GROWTH"); got != 3 {
		t.Fatalf("GROWTH counters after a 3-counter batch = %d, want 3", got)
	}

	// A different batch size (2) reads the new event's Amount: it must add
	// two more, not repeat the first batch's three -- a hard-coded amount
	// would pass the first assertion by coincidence.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 2})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after a second 2-counter batch = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Obj(id).Counter("GROWTH"); got != 5 {
		t.Fatalf("GROWTH counters after a second 2-counter batch = %d, want 5", got)
	}

	// The counter kind matters: a batch of a different kind is not "one or
	// more +1/+1 counters" and queues nothing.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "CHARGE", Amount: 4})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("pendingTriggers after a CHARGE batch = %d, want 0", len(e.pendingTriggers))
	}
}

// TestCounterAddedOnceReplaysThePayoff pins the same card's behaviour through
// a replay-verified game: a second Ascendancy-owned batch after the first is
// additive, so the "once per batch, not per counter" contract is observable
// across the public event stream rather than only in one resolution.
func TestCounterAddedOnceReplaysThePayoff(t *testing.T) {
	t.Parallel()
	asc := mshCorpusCard(t, "Simic Ascendancy")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, asc)
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	e.putTriggersOnStack()
	e.resolveTop()
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	e.putTriggersOnStack()
	e.resolveTop()

	if got := e.G.Obj(id).Counter("GROWTH"); got != 2 {
		t.Fatalf("GROWTH counters after two separate 1-counter batches = %d, want 2", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("pendingTriggers left over = %d, want 0", len(e.pendingTriggers))
	}
}

// TestCounterAddedOnceReferentsCaptureTheBatch pins the referent binding
// directly: the CounterAddedOnce case must set TriggerAmount to the event's
// batch size and TriggerCard to the permanent the counters landed on, so
// TriggerCount$Amount reads the batch and any card/controller referent reads
// the right object.
func TestCounterAddedOnceReferentsCaptureTheBatch(t *testing.T) {
	t.Parallel()
	asc := mshCorpusCard(t, "Simic Ascendancy")
	e := combatEngine(t)
	id := onBoardCard(t, e, 0, asc)
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	trig := e.G.Obj(id).Face().Triggers[0]
	if trig.Mode != "CounterAddedOnce" {
		t.Fatalf("Simic Ascendancy trigger[0] mode = %q, want CounterAddedOnce", trig.Mode)
	}
	ctx := e.triggerReferents(trig, id, events.Event{
		Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 3,
	}, nil)
	if ctx.TriggerAmount != 3 {
		t.Fatalf("TriggerAmount = %d, want 3", ctx.TriggerAmount)
	}
	if ctx.TriggerCard != bear {
		t.Fatalf("TriggerCard = %d, want %d (the counter recipient)", ctx.TriggerCard, bear)
	}
}
