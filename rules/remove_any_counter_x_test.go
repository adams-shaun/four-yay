package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMoxiteRefineryAnyCounterXSpansObjects pins the real Moxite Refinery's
// X/Any cost at the aggregate counter capacity. Its two eligible artifact
// creatures each carry two +1/+1 counters, so X=4 must be offered and paid
// by selecting one counter unit from each object twice.
func TestMoxiteRefineryAnyCounterXSpansObjects(t *testing.T) {
	t.Parallel()
	e, _, p := subCounterConfig(t, 107, "Moxite Refinery", "Walking Ballista", "Hangarback Walker")
	moxite := bridgeToHand(t, e, "Moxite Refinery")
	ballista := bridgeToHand(t, e, "Walking Ballista")
	hangarback := bridgeToHand(t, e, "Hangarback Walker")
	placeOnBattlefield(t, e, moxite)
	placeOnBattlefield(t, e, ballista)
	placeOnBattlefield(t, e, hangarback)
	for _, card := range []struct {
		name string
		id   state.ObjID
	}{
		{"Walking Ballista", ballista},
		{"Hangarback Walker", hangarback},
	} {
		e.emit(events.Event{Kind: events.CounterChange, Obj: card.id, Counter: "P1P1", Amount: 2})
		if got := e.G.Obj(card.id).Counter("P1P1"); got != 2 {
			t.Fatalf("precondition: %s has %d +1/+1 counters, want 2", card.name, got)
		}
	}
	for _, id := range []state.ObjID{moxite, ballista, hangarback} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d = %+v, want battlefield", id, o)
		}
	}
	e.pending = nil
	e.priorityRound()
	addMana(t, e, p, "GG") // Moxite's {2}; X is paid in counters.
	submitChoices(t, e, abilityOption(t, e, moxite, 0).Index)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("Moxite X decision = %+v, want an X choice", d)
	}
	xFour := -1
	for _, o := range d.Options {
		if o.Amount == 4 {
			xFour = o.Index
		}
	}
	if xFour < 0 {
		t.Fatalf("Moxite X options = %+v, want aggregate X=4", d.Options)
	}
	submitChoices(t, e, xFour)

	// Each per-unit ask must still offer the selected object until its second
	// counter is paid; the payment intentionally spans the two objects. The
	// fourth unit has only Hangarback remaining, so the payment flow records it
	// automatically rather than posing a one-option decision.
	for unit, id := range []state.ObjID{ballista, hangarback, ballista} {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("counter-unit %d decision = %+v, want a KChoose", unit+1, d)
		}
		pick := -1
		for _, o := range d.Options {
			if o.Obj == id && o.Counter == "P1P1" {
				pick = o.Index
			}
		}
		if pick < 0 {
			t.Fatalf("+1/+1 payment choice for object %d missing: %+v", id, d.Options)
		}
		submitChoices(t, e, pick)
	}
	if got := e.G.Obj(ballista).Counter("P1P1"); got != 0 {
		t.Fatalf("Ballista +1/+1 counters after payment = %d, want 0", got)
	}
	if got := e.G.Obj(hangarback).Counter("P1P1"); got != 0 {
		t.Fatalf("Hangarback +1/+1 counters after payment = %d, want 0", got)
	}
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("activation did not settle after four counter units: %+v", d)
	}
}
