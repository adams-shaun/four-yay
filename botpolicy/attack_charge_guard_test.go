package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLegalAttackChoicesDropsTapObligations pins the KAttackers charge guard's
// tap half: a positive Option.CostTaps is an obligation the policy cannot
// verify, so the pair is dropped exactly as the KBlockers guard drops one.
func TestLegalAttackChoicesDropsTapObligations(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1},
		{Index: 1, Kind: "attacker", Obj: 2, CostTaps: 1},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 20}}
	if got := LegalAttackChoices(b, d, []int{0, 1}); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("choices = %v, want [0] (the tap-costed pair dropped)", got)
	}
}

// TestLegalAttackChoicesTrimsOverBudgetLife pins the life half: the cumulative
// CostLife of the kept pairs must not exceed the acting player's life total,
// earliest kept.
func TestLegalAttackChoicesTrimsOverBudgetLife(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostLife: 2},
		{Index: 1, Kind: "attacker", Obj: 2, CostLife: 2},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 3}}
	if got := LegalAttackChoices(b, d, []int{0, 1}); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("choices = %v, want [0] (the second pair exceeds 3 life)", got)
	}
}

// TestLegalAttackChoicesLeavesChargeFreeAnswersUntouched pins the byte-
// identical ordinary case: with no non-mana charge the guard is inert.
func TestLegalAttackChoicesLeavesChargeFreeAnswersUntouched(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1, Value: 1},
		{Index: 1, Kind: "attacker", Obj: 2, Value: 1},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 20}}
	if got := LegalAttackChoices(b, d, []int{0, 1}); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want both unchanged", got)
	}
}
