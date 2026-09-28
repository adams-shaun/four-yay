package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestBairdEndStepTriggerUsesPowerComparedWithBasePower pins the real corpus
// IsPresent$ Creature.YouCtrl+powerGTbasePower condition. The two arms differ
// only in whether the controlled creature has a +1/+1 counter.
func TestBasePowerPredicateUsesLayerSevenBSet(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	setter := onBoard(t, e, 0, "Name:Setter\nTypes:Enchantment\nS:Mode$ Continuous | Affected$ Creature.Other | SetPower$ 4 | SetToughness$ 3 | Description$ set base P/T\nOracle:x\n")
	bear := onBoard(t, e, 0, "Name:Set Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(setter).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("precondition: layer-7b setter and candidate must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	d := e.Derived(bear)
	if d.BasePower != 4 || d.BaseToughness != 3 || d.Power != 5 || d.Toughness != 4 {
		t.Fatalf("precondition: set/counter candidate base %d/%d current %d/%d, want base 4/3 and current 5/4", d.BasePower, d.BaseToughness, d.Power, d.Toughness)
	}
	if !e.matchesSpec("Creature.basePowerEQ4", bear, effects.SpecContext{}) {
		t.Error("basePowerEQ4 must read the layer-7b SetPower value")
	}
	if e.matchesSpec("Creature.basePowerEQ5", bear, effects.SpecContext{}) {
		t.Error("basePowerEQ5 must not read the later +1/+1 counter as base power")
	}
	if !e.matchesSpec("Creature.powerGTbasePower", bear, effects.SpecContext{}) {
		t.Error("powerGTbasePower must compare the counter-modified value against the layer-7b base")
	}
}

func TestBairdEndStepTriggerUsesPowerComparedWithBasePower(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bairdCard, ok := reg.Lookup("Baird, Argivian Recruiter")
	if !ok {
		t.Fatal("corpus fixture: Baird, Argivian Recruiter missing")
	}

	t.Run("unboosted creature does not trigger", func(t *testing.T) {
		assertBairdEndStepTrigger(t, bairdCard, false)
	})
	t.Run("boosted creature triggers", func(t *testing.T) {
		assertBairdEndStepTrigger(t, bairdCard, true)
	})
}

func TestSwordOfTheSqueakCountsBasePowerOrToughnessOne(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	swordCard, ok := reg.Lookup("Sword of the Squeak")
	if !ok {
		t.Fatal("corpus fixture: Sword of the Squeak missing")
	}
	e := layerEngine(t)
	sword := onBoardCard(t, e, 0, swordCard)
	baseOne := onBoard(t, e, 0, "Name:Base One\nTypes:Creature\nPT:1/2\nOracle:x\n")
	baseTwo := onBoard(t, e, 0, "Name:Base Two\nTypes:Creature\nPT:2/1\nOracle:x\n")
	bearer := onBoard(t, e, 0, "Name:Bearer\nTypes:Creature\nPT:3/3\nOracle:x\n")
	if e.G.Obj(sword).Zone != state.ZBattlefield || e.G.Obj(baseOne).Zone != state.ZBattlefield || e.G.Obj(baseTwo).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield {
		t.Fatal("precondition: Sword and all three creatures must be on the battlefield")
	}
	if one, two := e.Derived(baseOne), e.Derived(baseTwo); one.BasePower != 1 || one.BaseToughness != 2 || two.BasePower != 2 || two.BaseToughness != 1 {
		t.Fatalf("precondition: base-one P/T %d/%d, base-two P/T %d/%d; want 1/2 and 2/1", one.BasePower, one.BaseToughness, two.BasePower, two.BaseToughness)
	}
	e.emit(events.Event{Kind: events.Attach, Obj: sword, IDs: []state.ObjID{bearer}})
	if e.G.Obj(sword).AttachedTo != bearer {
		t.Fatalf("precondition: Sword attached to %d, want bearer %d", e.G.Obj(sword).AttachedTo, bearer)
	}
	if got := e.Power(bearer); got != 5 {
		t.Fatalf("Sword bearer power = %d, want 5 (two creatures meet the base-1 filter)", got)
	}
	if got := e.Toughness(bearer); got != 5 {
		t.Fatalf("Sword bearer toughness = %d, want 5 (two creatures meet the base-1 filter)", got)
	}
}

func assertBairdEndStepTrigger(t *testing.T, bairdCard *cards.Card, boost bool) {
	t.Helper()
	e := layerEngine(t)
	baird := onBoardCard(t, e, 0, bairdCard)
	creature := onBoard(t, e, 0, "Name:Vanilla Creature\nTypes:Creature Human\nPT:2/2\nOracle:x\n")
	if e.G.Obj(baird).Zone != state.ZBattlefield || e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatal("precondition: Baird and its controlled creature must be on the battlefield")
	}
	if boost {
		e.emit(events.Event{Kind: events.CounterChange, Obj: creature, Counter: "P1P1", Amount: 1})
	}
	derived := e.Derived(creature)
	wantPower := int32(2)
	if boost {
		wantPower = 3
	}
	if derived.BasePower != 2 || derived.Power != wantPower {
		t.Fatalf("precondition: creature base/current power %d/%d, want 2/%d", derived.BasePower, derived.Power, wantPower)
	}
	if boost && derived.Power <= derived.BasePower {
		t.Fatalf("precondition: boosted creature power %d must exceed base %d", derived.Power, derived.BasePower)
	}
	if !boost && derived.Power != derived.BasePower {
		t.Fatalf("precondition: unboosted creature power %d must equal base %d", derived.Power, derived.BasePower)
	}

	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	got := queuedPhaseTriggers(e, baird)
	want := 0
	if boost {
		want = 1
	}
	if got != want {
		t.Fatalf("Baird end-step triggers = %d, want %d (creature power/base %d/%d)", got, want, derived.Power, derived.BasePower)
	}
}
