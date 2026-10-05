package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A real PutCounterAll resolution is one placement action even though it
// emits a CounterChange for each planeswalker. Bioessence Hydra must get one
// trigger carrying the sum of those placements, not one trigger per walker.
func TestCounterAddedAllPutCounterAllIsOneAction(t *testing.T) {
	t.Parallel()
	e, _ := putCounterTable(t, 20261005,
		[]*cards.Card{mshCorpusCard(t, "Bioessence Hydra"), card(t, "Name:Counter Sweeper\nTypes:Enchantment\nA:AB$ PutCounterAll | Cost$ 0 | ValidCards$ Planeswalker.YouCtrl | CounterType$ LOYALTY | CounterNum$ 2 | SpellDescription$ Put loyalty counters on your planeswalkers.\nOracle:x\n"),
			card(t, "Name:First Walker\nTypes:Planeswalker\nPT:4\nOracle:x\n"),
			card(t, "Name:Second Walker\nTypes:Planeswalker\nPT:4\nOracle:x\n")}, nil)
	hydra := findAndMoveToBattlefield(t, e, 0, "Bioessence Hydra")
	sweeper := findAndMoveToBattlefield(t, e, 0, "Counter Sweeper")
	first := findAndMoveToBattlefield(t, e, 0, "First Walker")
	second := findAndMoveToBattlefield(t, e, 0, "Second Walker")
	for _, id := range []state.ObjID{hydra, sweeper, first, second} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not on the battlefield", id)
		}
	}

	e.priorityRound()
	opt := abilityOption(t, e, sweeper, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)

	var hydraTriggers int
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == hydra {
			hydraTriggers++
		}
	}
	if hydraTriggers != 1 {
		t.Fatalf("PutCounterAll queued %d Hydra triggers for one two-recipient action, want 1", hydraTriggers)
	}
	if got := e.G.Obj(hydra).Counter("P1P1"); got != 4 {
		t.Fatalf("Hydra received %d P1P1 counters, want aggregate TriggerCount$Amount 4", got)
	}
	if got := e.G.Obj(first).Counter("LOYALTY"); got != 2 {
		t.Fatalf("first walker has %d loyalty counters, want 2", got)
	}
	if got := e.G.Obj(second).Counter("LOYALTY"); got != 2 {
		t.Fatalf("second walker has %d loyalty counters, want 2", got)
	}
}

// CounterTypeAddedAll's FirstTime$ latch is per recipient, while the action
// latch is per trigger line. A mixed action therefore captures both newly
// qualifying objects in one trigger, and TriggeredObjectLKICopy's plural
// referent applies Stalwart Successor's effect to both.
func TestCounterTypeAddedAllStalwartCapturesAllFirstTimeRecipients(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	stalwart := onBoardCard(t, e, 0, mshCorpusCard(t, "Stalwart Successor"))
	first := onBoard(t, e, 0, "Name:First Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	second := onBoard(t, e, 0, "Name:Second Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(stalwart).Zone != state.ZBattlefield || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatal("precondition: Stalwart and both recipient creatures must be on the battlefield")
	}
	before := len(e.pendingTriggers)
	e.BeginActionBatch()
	e.emit(events.Event{Kind: events.CounterChange, Obj: first, Counter: "P1P1", Amount: 2})
	e.emit(events.Event{Kind: events.CounterChange, Obj: second, Counter: "LOYALTY", Amount: 1})
	e.EndActionBatch()
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("one mixed-counter multi-recipient action queued %d Stalwart triggers, want 1", got)
	}
	pt := e.pendingTriggers[before]
	if got := len(pt.Ctx.Remembered); got != 2 {
		t.Fatalf("Stalwart captured %d qualifying recipients, want 2", got)
	}
	if pt.Ctx.Remembered[0].Obj != first || pt.Ctx.Remembered[1].Obj != second {
		t.Fatalf("Stalwart captured recipients %v, want [%d %d]", pt.Ctx.Remembered, first, second)
	}
	firstBefore := e.G.Obj(first).Counter("P1P1")
	secondBefore := e.G.Obj(second).Counter("P1P1")
	e.putTriggersOnStack()
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	for _, check := range []struct {
		id     state.ObjID
		before int32
	}{{first, firstBefore}, {second, secondBefore}} {
		if got := e.G.Obj(check.id).Counter("P1P1"); got != check.before+1 {
			t.Fatalf("Stalwart's TriggeredObjectLKICopy effect left recipient %d with %d P1P1 counters, want %d", check.id, got, check.before+1)
		}
	}
}

func TestCounterAddedAllCloakedCadetActivationLimit(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	cadet := onBoardCard(t, e, 0, mshCorpusCard(t, "Cloaked Cadet"))
	first := onBoard(t, e, 0, "Name:First Human\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	second := onBoard(t, e, 0, "Name:Second Human\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	if e.G.Obj(cadet).Zone != state.ZBattlefield || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatal("precondition: Cadet and both Human recipients must be on the battlefield")
	}
	before := len(e.pendingTriggers)
	e.emit(events.Event{Kind: events.CounterChange, Obj: first, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.CounterChange, Obj: second, Counter: "P1P1", Amount: 1})
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("two separate qualifying actions queued %d Cadet triggers, want 1 due to ActivationLimit$", got)
	}
}

func TestCounterAddedAllInvisibleWomanValidSource(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	sue := onBoardCard(t, e, 0, mshCorpusCard(t, "Invisible Woman, Sue Storm"))
	hero := onBoard(t, e, 0, "Name:Test Hero\nTypes:Creature Hero\nPT:2/2\nOracle:x\n")
	if e.G.Obj(sue).Zone != state.ZBattlefield || e.G.Obj(hero).Zone != state.ZBattlefield {
		t.Fatal("precondition: Sue and the other Hero must be on the battlefield")
	}
	before := len(e.pendingTriggers)
	previous := e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: hero, Counter: "P1P1", Amount: 1})
	e.SetCounterAdder(previous)
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("own counter placement queued %d Sue triggers, want 1", got)
	}
	previous = e.SetCounterAdder(1)
	e.emit(events.Event{Kind: events.CounterChange, Obj: hero, Counter: "P1P1", Amount: 1})
	e.SetCounterAdder(previous)
	if got := len(e.pendingTriggers) - before; got != 1 {
		t.Fatalf("opponent's counter placement queued %d total Sue triggers, want only the own-placement trigger", got)
	}
}
