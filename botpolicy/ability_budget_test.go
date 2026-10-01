package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAbilityBudgetEndsTheUntapCycle pins A5: a source already activated
// maxActivationsPerTurn times this turn (Basalt Monolith's "{3}: Untap this
// artifact" re-enabling its own tap forever) is never activated again, so
// the priority policy falls through to pass and the turn ends. Below the
// budget the same option still ranks as worth taking, so the rule is a
// budget, not a blanket ban.
func TestAbilityBudgetEndsTheUntapCycle(t *testing.T) {
	d := &decision.Decision{
		Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "ability", Obj: 9, Label: "Basalt Monolith: Untap this artifact."},
			{Index: 1, Kind: "pass"},
		},
	}
	b := Board{
		IsMain: true,
		Life:   TableOf(map[state.PlayerID]int32{0: 20, 1: 20}),
		// One own creature, so A1's broad no-own-creature equip decline (the
		// documented approximation that declines every "ability" while the
		// bot controls no creatures) does not mask the budget rule.
		Creatures: TableOf(map[state.ObjID]Creature{5: {Controller: 0}}),
		Cards:     TableOf(map[state.ObjID]Card{9: {Activated: maxActivationsPerTurn - 1}}),
	}
	if pick := b.chooseAbility(d); pick != 0 {
		t.Fatalf("below the budget: chooseAbility = %d, want 0 (the untap still ranks)", pick)
	}
	b.Cards.Set(9, Card{Activated: maxActivationsPerTurn})
	if pick := b.chooseAbility(d); pick != -1 {
		t.Fatalf("at the budget: chooseAbility = %d, want -1 (pass fallthrough)", pick)
	}
}
