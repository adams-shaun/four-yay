package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDawnhandDissidentRaiseCostRemoveAnyCounter pins its real-corpus
// exile permission through the RaiseCost$ RemoveAnyCounter payment, including
// the payer's choice of which controlled creature loses the counters.
func TestDawnhandDissidentRaiseCostRemoveAnyCounter(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Dawnhand Dissident"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"),
	)
	dawnhand := e.G.Zone(state.ZHand, 0)[0]
	bearA, bearB, spell := e.G.Zone(state.ZHand, 0)[1], e.G.Zone(state.ZHand, 0)[2], e.G.Zone(state.ZHand, 0)[3]
	placeOnBattlefield(t, e, dawnhand)
	placeOnBattlefield(t, e, bearA)
	placeOnBattlefield(t, e, bearB)

	// Construct Dawnhand's legal result state: an owned creature in exile
	// with Dawnhand as the recorded exiling source. The continuous static
	// reads both pieces of provenance when offering the play.
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZHand, To: state.ZExile, IDs: []state.ObjID{dawnhand}})
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZExile || o.Owner != 0 || o.ExiledWith != dawnhand || o.Face() == nil || !o.Face().IsCreature() {
		t.Fatalf("precondition: exiled cast card=%+v, want owned creature exiled with Dawnhand %d", o, dawnhand)
	}
	for _, id := range []state.ObjID{bearA, bearB} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Owner != 0 || !o.Face().IsCreature() {
			t.Fatalf("precondition: counter carrier %d=%+v, want owned creature on battlefield", id, o)
		}
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearA, Counter: "P1P1", Amount: 4})
	e.emit(events.Event{Kind: events.CounterChange, Obj: bearB, Counter: "P1P1", Amount: 3})
	e.pending = nil
	e.priorityRound()
	if gotA, gotB := e.G.Obj(bearA).Counter("P1P1"), e.G.Obj(bearB).Counter("P1P1"); gotA != 4 || gotB != 3 || gotA == gotB {
		t.Fatalf("precondition: counter totals A/B=%d/%d, want distinct totals 4/3", gotA, gotB)
	}
	// Dawnhand is itself a controlled creature but carries no counters; the
	// two Bears therefore make the payment decision genuinely choice-bearing.
	addMana(t, e, 0, "1G")
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending=%+v, want priority", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell && o.Mode == "mayplay" {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("Dawnhand may-play cast not offered for the exiled creature: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 {
		t.Fatalf("RemoveAnyCounter payment ask=%+v, want one counter-removal choice", d)
	}
	bearBOption := -1
	eligible := 0
	for _, o := range d.Options {
		if o.Kind == "subcounter" {
			eligible++
			if o.Obj == bearB {
				bearBOption = o.Index
			}
		}
	}
	if eligible != 2 || bearBOption < 0 {
		t.Fatalf("payment choices=%+v, want both counter-bearing Bears including chosen Bear %d", d.Options, bearB)
	}
	submitChoices(t, e, bearBOption)
	// Any counters are paid one unit per answer; keep choosing the same
	// eligible Bear until the three-unit surcharge is settled.
	for i := 0; i < 3; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		option := -1
		for _, o := range d.Options {
			if o.Kind == "subcounter" && o.Obj == bearB {
				option = o.Index
				break
			}
		}
		if option < 0 {
			break
		}
		submitChoices(t, e, option)
	}
	if len(e.G.Stack) == 0 || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("precondition after counter answer: stack=%v spell zone=%v pending=%+v", e.G.Stack, e.G.Obj(spell).Zone, e.Pending())
	}
	if !hasEvent(e, events.PutOnStack, spell) {
		t.Fatalf("spell %d reached the stack without a PutOnStack cast event", spell)
	}
	passUntilStackEmpty(t, e, 30)
	if got := e.G.Obj(bearA).Counter("P1P1"); got != 4 {
		t.Fatalf("unchosen Bear has %d counters, want 4", got)
	}
	if got := e.G.Obj(bearB).Counter("P1P1"); got != 0 {
		t.Fatalf("chosen Bear has %d counters after payment, want 0 (3 removed); pending=%+v changes=%v", got, e.Pending(), hasCounterChange(e, bearB, "P1P1", -3))
	}
	if !hasCounterChange(e, bearB, "P1P1", -3) {
		t.Fatalf("no CounterChange -3 P1P1 on chosen Bear %d", bearB)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("cast spell zone=%v, want battlefield after successful resolution", o)
	}
}
