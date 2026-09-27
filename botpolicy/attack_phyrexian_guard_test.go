package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLegalAttackChoicesTrimsCombinedPhyrexianTax pins the KAttackers charge
// guard's pip half: with Norn's Annex out each pair costs {W/P}, the wire
// publishes one pip per pair, and the cumulative life branch (two per pip)
// must fit the acting player's life. At 4 life only two of three pairs are
// kept, so the policy never declares a combined pip tax the engine rejects
// (a rejected bot intent crashes the match).
func TestLegalAttackChoicesTrimsCombinedPhyrexianTax(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Max: 3, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 1, CostPhyrexian: 1},
		{Index: 1, Kind: "attacker", Obj: 2, CostPhyrexian: 1},
		{Index: 2, Kind: "attacker", Obj: 3, CostPhyrexian: 1},
	}}
	b := Board{Life: map[state.PlayerID]int32{0: 4}}
	if got := LegalAttackChoices(b, d, []int{0, 1, 2}); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want [0 1] (third pip exceeds 4 life)", got)
	}
}
