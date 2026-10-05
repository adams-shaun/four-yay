package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trig:CostedExecutor ticket (costedexec1). Forge's "you may pay {N}. If
// you do, ..." trigger idiom is a `T:Mode$ ... | Execute$ SVar` whose SVar
// body is an AB$ carrying a Cost$. The triggered-cost window (trigcost1) runs
// the pay/decline election for such a body, but its payable gate only knew
// the mana/life half and the component families -- a PayEnergy<N> part was
// unPriceable with no settle, so the window offered DECLINE ONLY and the body
// ran for free on a decline (the defect this ticket closes). These tests pin
// the real Creative Energy Commander carriers end to end on the compiled
// corpus: the energy is charged on pay and untouched on decline, and a payer
// who cannot cover the cost is offered no "pay" at all.
//
// None of the carriers is in any legacy golden deck, so TestHeads does not
// depend on their behaviour.

// costedEnergyWindow drives until the trigger-cost window ask is pending and
// returns the pending decision.
func costedEnergyWindow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := passUntilNonPriority(t, e, 80)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected the trigger-cost window ask, got %+v", d)
	}
	return d
}

// costedEnergyPayDecline returns the pay and decline option indices in a
// window ask, or -1 when the option is absent.
func costedEnergyPayDecline(d *decision.Decision) (pay, decline int) {
	pay, decline = -1, -1
	for _, o := range d.Options {
		switch o.Kind {
		case "trigger_cost_pay":
			pay = o.Index
		case "trigger_cost_decline":
			decline = o.Index
		}
	}
	return pay, decline
}

// answerRocPlacementChainAsk answers Aetherstorm Roc's chained `DB$ Tap |
// TargetMin$ 0` announcement, which CR 603.3d poses as each attacking Roc's
// trigger is put on the stack (KTarget, ResumeKind "trig_sub") rather than
// mid-resolution. The link is optional, so the test declines it; the costed
// body this file is about then resolves and opens the pay/decline window.
func answerRocPlacementChainAsk(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "trig_sub" {
		t.Fatalf("expected the Roc's placement chain ask, got %+v", d)
	}
	if d.Min != 0 {
		t.Fatalf("the optional DB$ Tap link reports Min %d, want 0 so the test can decline", d.Min)
	}
	submitChoices(t, e)
}

// drainCostedStack drains the stack after a paid body, answering any
// mid-resolution target ask with its empty answer when the ask permits one
// (Aetherstorm Roc's chained `DB$ Tap | TargetMin$ 0` is the carrier: after
// paying, the optional tap of a defending creature is the last decision).
func drainCostedStack(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining the stack (depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KPriority {
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			submitChoices(t, e, idx)
			continue
		}
		if d.Min == 0 {
			submitChoices(t, e)
			continue
		}
		submitChoices(t, e, 0)
	}
}

// TestOverclockedElectromancerPaysEnergyAndAddsCounter pins the pay arm on the
// real Phase-trigger carrier: paying {E}{E}{E} spends exactly three energy
// and puts the +1/+1 counter on it.
func TestOverclockedElectromancerPaysEnergyAndAddsCounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Overclocked Electromancer")
	id := searchMoveByName(t, e, "Overclocked Electromancer", state.ZBattlefield)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 3})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)

	d := costedEnergyWindow(t, e)
	pay, _ := costedEnergyPayDecline(d)
	if pay < 0 {
		t.Fatalf("the PayEnergy<3> body was not offered as payable: %+v", d.Options)
	}
	before := e.G.Obj(id).Counter("P1P1")
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy = %d after paying {E}{E}{E}, want 0", got)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != before+1 {
		t.Fatalf("+1/+1 counters = %d, want %d (the paid body must run)", got, before+1)
	}
}

// TestOverclockedElectromancerDeclineKeepsEnergyAndCounter pins the decline
// arm: a declined body neither spends energy nor adds the counter.
func TestOverclockedElectromancerDeclineKeepsEnergyAndCounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Overclocked Electromancer")
	id := searchMoveByName(t, e, "Overclocked Electromancer", state.ZBattlefield)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 3})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)

	d := costedEnergyWindow(t, e)
	_, decline := costedEnergyPayDecline(d)
	if decline < 0 {
		t.Fatalf("the window never offered a decline: %+v", d.Options)
	}
	before := e.G.Obj(id).Counter("P1P1")
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[0].Counter("ENERGY"); got != 3 {
		t.Fatalf("energy = %d after declining, want the untouched 3", got)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != before {
		t.Fatalf("+1/+1 counters = %d after declining, want unchanged %d", got, before)
	}
}

// TestOverclockedElectromancerEnergyShortOffersNoPay pins the affordability
// gate: with two energy (one short of {E}{E}{E}) the window poses the ask but
// offers NO "pay" option, so a free body is unreachable.
func TestOverclockedElectromancerEnergyShortOffersNoPay(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Overclocked Electromancer")
	id := searchMoveByName(t, e, "Overclocked Electromancer", state.ZBattlefield)
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 2})
	driveToStep(t, e, e.G.Turn, e.G.Active, state.StepBeginCombat)

	d := costedEnergyWindow(t, e)
	pay, decline := costedEnergyPayDecline(d)
	if pay >= 0 {
		t.Fatalf("two energy must not offer the {E}{E}{E} pay: %+v", d.Options)
	}
	if decline < 0 {
		t.Fatalf("the window must still offer the decline: %+v", d.Options)
	}
	before := e.G.Obj(id).Counter("P1P1")
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("energy = %d, want the untouched 2", got)
	}
	if got := e.G.Obj(id).Counter("P1P1"); got != before {
		t.Fatalf("+1/+1 counters = %d, want unchanged %d", got, before)
	}
}

// TestAetherstormRocAttackPaysEnergyAndAddsCounter pins the attack-trigger
// carrier (a mandatory AB$ PutCounter with a chained DB$ Tap sub-ability): pay
// {E}{E} charges two energy and puts the +1/+1 counter on the Roc.
func TestAetherstormRocAttackPaysEnergyAndAddsCounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := combatEngine(t)
	roc := onBoardCard(t, e, 0, searchCorpusCard(t, reg, "Aetherstorm Roc"))
	e.G.Obj(roc).SummonSick = false
	e.G.Active = 0
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 2})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{roc}})
	e.putTriggersOnStack()
	answerRocPlacementChainAsk(t, e)

	d := costedEnergyWindow(t, e)
	pay, _ := costedEnergyPayDecline(d)
	if pay < 0 {
		t.Fatalf("the PayEnergy<2> body was not offered as payable: %+v", d.Options)
	}
	before := e.G.Obj(roc).Counter("P1P1")
	submitChoices(t, e, pay)
	drainCostedStack(t, e, 40)

	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy = %d after paying {E}{E}, want 0", got)
	}
	if got := e.G.Obj(roc).Counter("P1P1"); got != before+1 {
		t.Fatalf("+1/+1 counters = %d, want %d (the paid body must run)", got, before+1)
	}
}

// TestAetherstormRocAttackDeclineKeepsEnergyAndCounter is the decline half.
func TestAetherstormRocAttackDeclineKeepsEnergyAndCounter(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := combatEngine(t)
	roc := onBoardCard(t, e, 0, searchCorpusCard(t, reg, "Aetherstorm Roc"))
	e.G.Obj(roc).SummonSick = false
	e.G.Active = 0
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 2})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{roc}})
	e.putTriggersOnStack()
	answerRocPlacementChainAsk(t, e)

	d := costedEnergyWindow(t, e)
	_, decline := costedEnergyPayDecline(d)
	if decline < 0 {
		t.Fatalf("the window never offered a decline: %+v", d.Options)
	}
	before := e.G.Obj(roc).Counter("P1P1")
	submitChoices(t, e, decline)
	drainCostedStack(t, e, 40)

	if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("energy = %d after declining, want the untouched 2", got)
	}
	if got := e.G.Obj(roc).Counter("P1P1"); got != before {
		t.Fatalf("+1/+1 counters = %d after declining, want unchanged %d", got, before)
	}
}

// TestVoltaicBrawlerAttackPaysEnergyAndPumps covers a different API sibling
// (AB$ Pump | Cost$ PayEnergy<1>): the same shared gate must charge the energy
// and run the +1/+1 body, proving the fix is not special-cased to PutCounter.
func TestVoltaicBrawlerAttackPaysEnergyAndPumps(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e := combatEngine(t)
	brawler := onBoardCard(t, e, 0, searchCorpusCard(t, reg, "Voltaic Brawler"))
	e.G.Obj(brawler).SummonSick = false
	e.G.Active = 0
	if d := e.Derived(brawler); d.Power != 3 || d.Toughness != 2 {
		t.Fatalf("precondition: base P/T = %d/%d, want 3/2", d.Power, d.Toughness)
	}
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: 1})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{brawler}})
	e.putTriggersOnStack()
	e.resolveTop()

	d := costedEnergyWindow(t, e)
	pay, _ := costedEnergyPayDecline(d)
	if pay < 0 {
		t.Fatalf("the PayEnergy<1> body was not offered as payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	drainCostedStack(t, e, 40)

	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy = %d after paying {E}, want 0", got)
	}
	if d := e.Derived(brawler); d.Power != 4 || d.Toughness != 3 {
		t.Fatalf("P/T = %d/%d after the paid pump, want 4/3", d.Power, d.Toughness)
	}
	if !e.HasKeyword(brawler, "Trample") {
		t.Fatal("the paid pump's Trample grant is missing")
	}
}

// carrier end to end: an artifact entering triggers the body, paying {1}
// spends one generic from the pool and grants {E}{E}.
func TestEraOfInnovationPaysManaAndGrantsEnergy(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Era of Innovation", "Ornithopter")
	searchMoveByName(t, e, "Era of Innovation", state.ZBattlefield)
	addMana(t, e, 0, "C")
	// The artifact's entry fires the ChangesZone trigger.
	searchMoveByName(t, e, "Ornithopter", state.ZBattlefield)

	d := costedEnergyWindow(t, e)
	pay, _ := costedEnergyPayDecline(d)
	if pay < 0 {
		t.Fatalf("the Cost$ 1 body was not offered as payable: %+v", d.Options)
	}
	poolBefore := e.G.Players[0].Pool[state.MC]
	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[0].Pool[state.MC]; got != poolBefore-1 {
		t.Fatalf("generic pool = %d after paying {1}, want %d", got, poolBefore-1)
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 2 {
		t.Fatalf("energy = %d after the paid body, want 2 (the {E}{E} grant)", got)
	}
}

// TestEraOfInnovationDeclineKeepsManaAndGrantsNothing is the decline half.
func TestEraOfInnovationDeclineKeepsManaAndGrantsNothing(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Era of Innovation", "Ornithopter")
	searchMoveByName(t, e, "Era of Innovation", state.ZBattlefield)
	addMana(t, e, 0, "C")
	searchMoveByName(t, e, "Ornithopter", state.ZBattlefield)

	d := costedEnergyWindow(t, e)
	_, decline := costedEnergyPayDecline(d)
	if decline < 0 {
		t.Fatalf("the window never offered a decline: %+v", d.Options)
	}
	poolBefore := e.G.Players[0].Pool[state.MC]
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)

	if got := e.G.Players[0].Pool[state.MC]; got != poolBefore {
		t.Fatalf("generic pool = %d after declining, want the untouched %d", got, poolBefore)
	}
	if got := e.G.Players[0].Counter("ENERGY"); got != 0 {
		t.Fatalf("energy = %d after declining, want 0", got)
	}
}
