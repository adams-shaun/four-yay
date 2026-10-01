package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The budget arm for a budgeted hidden-library search (a KChoose with
// ResumeKind "search" carrying WithTotalCMC$, so Decision.MaxSum > 0): the
// arm fills to Min under the cap -- the exact mirror of the search's R-9
// stand-in, whose pick bound is Min (greedy[:min] for a quantity-only
// filter, whose Min the engine lowers to the greedy count; the empty
// fail-to-find for a stated-quality filter, whose Min stays 0). The dig /
// hidden_pick arm fills to Max because THOSE stand-ins take the greedy set
// to Max; the search's stand-in does not, so the bound differs.

// TestSearchBudgetArmStatedQualityDeclines: a stated-quality budget search
// (Min 0 -- CR 701.23b's fail-to-find allowance) is answered with the empty
// intent, the mirror of the stand-in's legitimate fail-to-find. The pre-fix
// arm took the first offer regardless of MaxSum, diverging from the
// stand-in it mirrors.
func TestSearchBudgetArmStatedQualityDeclines(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "search",
		Min:        0,
		Max:        2,
		MaxSum:     6,
		Options: []decision.Option{
			{Index: 0, Kind: "search", Obj: 1, Value: 4},
			{Index: 1, Kind: "search", Obj: 2, Value: 4},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if len(in.Choices) != 0 {
		t.Fatalf("choices = %v, want the empty decline (the stated-quality stand-in's fail-to-find)", in.Choices)
	}
}

// TestSearchBudgetArmQuantityOnlyTakesTheGreedySet: a quantity-only budget
// search's Min is the engine-lowered greedy count, so the fill to Min takes
// exactly the greedy set. Options priced [3,4,2], Min 2, Max 2, MaxSum 5:
// the 3 fits, the 4 does not after it (3+4 = 7 > 5), the 2 fits (sum 5) --
// [0,2], which passes Validate.
func TestSearchBudgetArmQuantityOnlyTakesTheGreedySet(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "search",
		Min:        2,
		Max:        2,
		MaxSum:     5,
		Options: []decision.Option{
			{Index: 0, Kind: "search", Obj: 1, Value: 3},
			{Index: 1, Kind: "search", Obj: 2, Value: 4},
			{Index: 2, Kind: "search", Obj: 3, Value: 2},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 2}) {
		t.Fatalf("choices = %v, want [0 2] (greedy fill: 3 fits, 4 does not, 2 fits)", in.Choices)
	}
}

// TestSearchBudgetArmUnchangedWithoutMaxSum: a budget-less search keeps the
// first-offer policy byte-for-byte.
func TestSearchBudgetArmUnchangedWithoutMaxSum(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:       decision.KChoose,
		ResumeKind: "search",
		Min:        0,
		Max:        1,
		Options: []decision.Option{
			{Index: 0, Kind: "search", Obj: 1},
			{Index: 1, Kind: "search", Obj: 2},
		},
	}
	in := Decide(b, &d, rng(1))
	if !reflect.DeepEqual(in.Choices, []int{0}) {
		t.Fatalf("choices = %v, want [0] (the first-offer policy unchanged)", in.Choices)
	}
}
