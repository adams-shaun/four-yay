package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// fb-20261006T065812Z-2782e8eb: a Phyrexian-cost cast (Dismember) got no
// PaymentAction because PlanCostDetail declined "cost:phyrexian", so the web
// had no CAST affordance. The witness binds only the mana half; the pips are
// answered afterwards through the CR 601.2b pip ask.

const phyrexianDismemberSrc = "Name:Dismember\nManaCost:1 BP BP\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ -5 | NumDef$ -5\nOracle:x\n"

const (
	phyrexianSeaSrc       = "Name:Underground Sea\nTypes:Land Island Swamp\nOracle:x\n"
	phyrexianWastelandSrc = "Name:Wasteland\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C\nOracle:x\n"
)

func dismemberAction(t *testing.T, e *Engine, spell state.ObjID) decision.PaymentAction {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("precondition: pending = %+v, want priority", d)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZHand || o.Face().Name != "Dismember" {
		t.Fatalf("precondition: spell %+v, want Dismember in hand", o)
	}
	d.PaymentActions = e.PaymentActionsForPriority(0, d.Seq)
	for _, a := range d.PaymentActions {
		if a.Cast.Object == spell {
			if len(a.Plans) == 0 {
				t.Fatalf("Dismember action has no plans: %+v", a)
			}
			return a
		}
	}
	t.Fatalf("no Dismember payment action in %+v", d.PaymentActions)
	return decision.PaymentAction{}
}

func submitPayment(t *testing.T, e *Engine, a decision.PaymentAction) {
	t.Helper()
	d := e.Pending()
	in := decision.Intent{Seq: d.Seq, Player: 0, Payment: &decision.PaymentSelection{ActionID: a.ID, Plan: a.Plans[0]}}
	if err := e.Submit(in); err != nil {
		t.Fatalf("Submit planned Dismember: %v", err)
	}
}

// The reporter's board: two Underground Sea and a Wasteland. The
// planner offers a Dismember action, ValidateCastPayment accepts its witness,
// and answering both pip asks with the life face completes the cast.
func TestPaymentPlanPhyrexianOffersAndPaysWithLife(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9570, phyrexianDismemberSrc, targetSrc)
	var lands []state.ObjID
	for _, src := range []string{phyrexianSeaSrc, phyrexianSeaSrc, phyrexianWastelandSrc} {
		id := onBoard(t, e, 0, src)
		e.G.Obj(id).SummonSick = false
		lands = append(lands, id)
	}
	target := putToken(t, e, 1, targetSrc, state.ZBattlefield)
	toMain1(t, e)
	e.pending = nil
	e.Advance()
	startLife := e.G.Players[0].Life
	if startLife <= 4 {
		t.Fatalf("precondition: life = %d, want more than the 4 two pips cost", startLife)
	}
	a := dismemberAction(t, e, spell)
	if err := e.ValidateCastPayment(0, a.Cast, a.Plans[0]); err != nil {
		t.Fatalf("ValidateCastPayment: %v", err)
	}
	submitPayment(t, e, a)
	for i := 0; i < 2; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("pip ask %d = %+v, want choose", i, d)
		}
		life := -1
		for _, o := range d.Options {
			if o.Kind == "pay_life" {
				life = o.Index
			}
		}
		if life < 0 {
			t.Fatalf("pip ask %d has no pay_life option: %+v", i, d.Options)
		}
		submitChoices(t, e, life)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after pips pending = %+v, want target", d)
	}
	ti := -1
	for _, o := range d.Options {
		if o.Obj == target {
			ti = o.Index
		}
	}
	if ti < 0 {
		t.Fatalf("target missing: %+v", d.Options)
	}
	submitChoices(t, e, ti)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Dismember zone = %s, want stack", e.G.Obj(spell).Zone)
	}
	// The witness funded only the generic {1}: exactly one source tapped.
	tapped := 0
	for _, id := range lands {
		if e.G.Obj(id).Tapped {
			tapped++
		}
	}
	if tapped != 1 {
		t.Fatalf("%d lands tapped, want exactly the one the witness named", tapped)
	}
	if got := e.G.Players[0].Life; got != startLife-4 {
		t.Fatalf("life = %d, want %d (two Phyrexian pips paid with 2 life each)", got, startLife-4)
	}
	// (no replayCheck: onBoard places the lands without events)
}

// With {B}{B}{C} floating the plan spends the pool; answering the colour face
// for both pips completes the cast without touching life.
func TestPaymentPlanPhyrexianPaysWithMana(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9571, phyrexianDismemberSrc, targetSrc)
	target := putToken(t, e, 1, targetSrc, state.ZBattlefield)
	var lands []state.ObjID
	for _, src := range []string{phyrexianSeaSrc, phyrexianSeaSrc, phyrexianWastelandSrc} {
		id := onBoard(t, e, 0, src)
		e.G.Obj(id).SummonSick = false
		lands = append(lands, id)
	}
	addMana(t, e, 0, "BBC")
	if e.G.Players[0].Pool[state.MB] != 2 || e.G.Players[0].Pool[state.MC] != 1 || len(lands) != 3 {
		t.Fatalf("precondition: pool=%v lands=%d, want BBC and three sources", e.G.Players[0].Pool, len(lands))
	}
	startLife := e.G.Players[0].Life
	a := dismemberAction(t, e, spell)
	if err := e.ValidateCastPayment(0, a.Cast, a.Plans[0]); err != nil {
		t.Fatalf("ValidateCastPayment: %v", err)
	}
	submitPayment(t, e, a)
	for i := 0; i < 2; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("pip ask %d = %+v, want choose", i, d)
		}
		face := -1
		for _, o := range d.Options {
			if o.Kind == "pay_B" {
				face = o.Index
			}
		}
		if face < 0 {
			t.Fatalf("pip ask %d has no pay_B option: %+v", i, d.Options)
		}
		submitChoices(t, e, face)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after pips pending = %+v, want target", d)
	}
	for _, o := range d.Options {
		if o.Obj == target {
			submitChoices(t, e, o.Index)
		}
	}
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Dismember zone = %s, want stack", e.G.Obj(spell).Zone)
	}
	if got := e.G.Players[0].Life; got != startLife {
		t.Fatalf("life = %d, want %d (colour face paid both pips)", got, startLife)
	}
	for _, id := range lands {
		if e.G.Obj(id).Tapped {
			t.Fatalf("pool-funded plan tapped source %d", id)
		}
	}
	// (onBoard places lands without events, so this setup is not replay-checked)
}

// The classifier admits only the Phyrexian pips: a cost that carries a
// Phyrexian pip AND another unsupported part still declines with that part's
// detail, and hybrid still declines.
func TestPaymentPlanPhyrexianClassifierStaysNarrow(t *testing.T) {
	t.Parallel()
	if got := pay.PlanCostDetail(pay.Cost{Generic: 1, Phyrexian: []byte{'B', 'B'}}); got != "" {
		t.Fatalf("pure Phyrexian cost detail = %q, want admitted", got)
	}
	if got := pay.PlanCostDetail(pay.Cost{Phyrexian: []byte{'B'}, X: 1}); got != "cost:x" {
		t.Fatalf("Phyrexian+X detail = %q, want cost:x", got)
	}
	if got := pay.PlanCostDetail(pay.Cost{Phyrexian: []byte{'B'}, Tap: true}); got != "cost:tap" {
		t.Fatalf("Phyrexian+tap detail = %q, want cost:tap", got)
	}
	e, _, spell := newFixtureDeck(t, 9572, "Name:Hybrid Spell\nManaCost:R/G\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoard(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan != nil || got.Reason != "unsupported" || got.Detail != "cost:hybrid" {
		t.Fatalf("hybrid outcome = %+v, want unsupported cost:hybrid", got)
	}
}
