package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestBaseSwordCountValidUsesLayerSevenBPredicate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Sword of the Squeak")
	if !ok {
		t.Fatal("corpus fixture: Sword of the Squeak missing")
	}
	for _, tc := range []struct {
		name                            string
		printed, setPower, setToughness int32
		wantBonus                       int32
	}{
		{name: "printed base-one set away", printed: 1, setPower: 4, setToughness: 3, wantBonus: 0},
		{name: "printed non-one set to base-one", printed: 2, setPower: 1, setToughness: 1, wantBonus: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			sword := onBoardCard(t, e, 0, card)
			candidate := onBoard(t, e, 0, fmt.Sprintf("Name:Candidate\nTypes:Creature\nPT:%d/2\nOracle:x\n", tc.printed))
			bearer := onBoard(t, e, 0, "Name:Bearer\nTypes:Creature\nPT:3/3\nOracle:x\n")
			if e.G.Obj(sword).Zone != state.ZBattlefield || e.G.Obj(candidate).Zone != state.ZBattlefield || e.G.Obj(bearer).Zone != state.ZBattlefield {
				t.Fatal("precondition: Sword, candidate, and bearer must be on the battlefield")
			}
			e.AddContinuous(state.ContinuousEffect{Source: candidate, Controller: 0, Layer: LPT, Sub: SubSet,
				Affects: "Creature.Self", HasSet: true, SetPower: tc.setPower, SetToughness: tc.setToughness})
			d := e.Derived(candidate)
			if d.BasePower != tc.setPower || d.BaseToughness != tc.setToughness || d.BasePower == tc.printed {
				t.Fatalf("precondition: printed power %d differs from layer-7b base %d/%d", tc.printed, d.BasePower, d.BaseToughness)
			}
			if bd := e.Derived(bearer); bd.Power != 3 || bd.Toughness != 3 {
				t.Fatalf("precondition: bearer baseline P/T = %d/%d, want 3/3", bd.Power, bd.Toughness)
			}
			e.emit(events.Event{Kind: events.Attach, Obj: sword, IDs: []state.ObjID{bearer}})
			if e.G.Obj(sword).AttachedTo != bearer {
				t.Fatalf("precondition: Sword attached to %d, want bearer %d", e.G.Obj(sword).AttachedTo, bearer)
			}
			want := int32(3) + tc.wantBonus
			if got := e.Power(bearer); got != want {
				t.Fatalf("Sword bearer power = %d, want %d from layer-7b base-one count", got, want)
			}
			if got := e.Toughness(bearer); got != want {
				t.Fatalf("Sword bearer toughness = %d, want %d from layer-7b base-one count", got, want)
			}
		})
	}
}

func TestBaseLayerSevenAffectedPredicateUsesWalkValues(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	candidate := onBoard(t, e, 0, "Name:Earlier set\nTypes:Creature\nPT:2/2\nOracle:x\n")
	faceDown := onBoard(t, e, 0, "Name:Printed four three\nTypes:Creature\nPT:4/3\nOracle:x\n")
	setter := onBoard(t, e, 0, "Name:Filter setter\nTypes:Enchantment\nOracle:x\n")
	if e.G.Obj(candidate).Zone != state.ZBattlefield || e.G.Obj(faceDown).Zone != state.ZBattlefield {
		t.Fatal("precondition: both layer-walk candidates must be on the battlefield")
	}
	e.AddContinuous(state.ContinuousEffect{Source: candidate, Controller: 0, Layer: LPT, Sub: SubSet,
		Affects: "Creature.Self", HasSet: true, SetPower: 4, SetToughness: 3})
	e.G.Obj(faceDown).FaceDown = true
	e.AddContinuous(state.ContinuousEffect{Source: setter, Controller: 0, Layer: LPT, Sub: SubSet,
		Affects: "Creature.basePowerEQ4+baseToughnessEQ3", HasSet: true, SetPower: 8, SetToughness: 8})
	gotSet, gotDown := e.Derived(candidate), e.Derived(faceDown)
	if gotSet.BasePower != 8 || gotSet.BaseToughness != 8 {
		t.Fatalf("precondition/result: earlier-set candidate base = %d/%d, want selected to 8/8", gotSet.BasePower, gotSet.BaseToughness)
	}
	if gotDown.BasePower != 2 || gotDown.BaseToughness != 2 {
		t.Fatalf("face-down candidate base = %d/%d, want 2/2 and excluded by the base filter", gotDown.BasePower, gotDown.BaseToughness)
	}
	if unknown := effects.UnknownPredicates("Creature.basePowerEQ4+baseToughnessEQ3"); len(unknown) != 0 {
		t.Fatalf("base predicate was reported unknown: %v", unknown)
	}
}
