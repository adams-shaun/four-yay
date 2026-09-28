package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestFirstStrikeDamagePhaseGate(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	e.G.Step = state.StepCombatDamage
	tr := cards.Trigger{Params: map[string]string{"Phase": "First Strike Damage"}}

	if e.phaseGate(tr) {
		t.Fatal("First Strike Damage matched without a first striker in combat")
	}

	first := onBoard(t, e, 0, "Name:First Guard\nTypes:Creature Soldier\nPT:2/2\nK:First Strike\nOracle:x\n")
	if obj := e.G.Obj(first); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("synthetic first striker is not on battlefield: %+v", obj)
	}
	if !e.HasKeyword(first, "First Strike") {
		t.Fatal("synthetic combat creature does not have First Strike")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: e.G.Active, IDs: []state.ObjID{first}})
	if !e.G.Obj(first).IsAttacking {
		t.Fatal("synthetic first striker was not declared as an attacker")
	}
	if !e.phaseGate(tr) {
		t.Fatal("First Strike Damage did not match with a first striker in combat")
	}
}
