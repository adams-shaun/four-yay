package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestDigBudgetArmFillsGreedilyUnderMaxSum: a Dig carrying a cumulative
// WithTotalCMC$ budget (Decision.MaxSum) must be answered with a set whose
// Value sum fits the budget -- a blind first-Max answer fails Validate and
// the engine rejects it, so the bot would livelock re-deriving it. The arm
// fills greedily in offered order: [2,3,2] under budget 4 takes the two
// 2-MV cards (3 does not fit after the first 2) and passes Validate.
func TestDigBudgetArmFillsGreedilyUnderMaxSum(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:   decision.KChoose,
		Min:    0,
		Max:    3,
		MaxSum: 4,
		Options: []decision.Option{
			{Index: 0, Kind: "dig", Obj: 1, Value: 2},
			{Index: 1, Kind: "dig", Obj: 2, Value: 3},
			{Index: 2, Kind: "dig", Obj: 3, Value: 2},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 2}) {
		t.Fatalf("choices = %v, want [0 2] (greedy fill: 2 fits, 3 does not, 2 fits)", in.Choices)
	}
}

// TestDigBudgetArmIndexAndCountBoundsAgree: the arm's fill is bounded by the
// run COUNT, not by the option index -- the exact shape effDig's forced
// greedy take uses. michelangelos_technique's real shape: ChangeNum$ 2,
// budget 6, options [4,4,2]. A 4 does not fit after a first 4, so the scan
// must skip index 1 and continue to index 2 (count fill [0,2]); an
// index-bounded arm stopped at index 2 and took [0] only, fewer cards than
// the no-choice stand-in. This is the regression guard the earlier [2,3,2]
// case masked, since there the bounds happen to agree.
func TestDigBudgetArmIndexAndCountBoundsAgree(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:   decision.KChoose,
		Min:    0,
		Max:    2,
		MaxSum: 6,
		Options: []decision.Option{
			{Index: 0, Kind: "dig", Obj: 1, Value: 4},
			{Index: 1, Kind: "dig", Obj: 2, Value: 4},
			{Index: 2, Kind: "dig", Obj: 3, Value: 2},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 2}) {
		t.Fatalf("choices = %v, want [0 2] (count fill: first 4 fits, second 4 skipped, 2 fits)", in.Choices)
	}
}

// TestClampBudgetAwareTopUp: a mandatory budget dig (Min > 0) whose policy
// answer came up short must be topped up to Min WITHIN the budget. Before
// this fix Clamp padded in plain index order, ignoring MaxSum: for
// Min 2/Max 2/MaxSum 3 over [1,3,1] it produced [0,1] (sum 4), which
// Decision.Validate rejects, and Engine.Submit rejects before consuming the
// pending decision -- so the deterministic bot re-derived the same invalid
// answer forever.
func TestClampBudgetAwareTopUp(t *testing.T) {
	d := decision.Decision{
		Kind:   decision.KChoose,
		Min:    2,
		Max:    2,
		MaxSum: 3,
		Options: []decision.Option{
			{Index: 0, Kind: "dig", Obj: 1, Value: 1},
			{Index: 1, Kind: "dig", Obj: 2, Value: 3},
			{Index: 2, Kind: "dig", Obj: 3, Value: 1},
		},
	}
	// A policy that returned nothing: Clamp must find [0,2] (1+1 <= 3),
	// never [0,1] (1+3 > 3).
	in := Clamp(&d, decision.Intent{})
	if err := d.Validate(in); err != nil {
		t.Fatalf("clamped answer %v failed Validate: %v", in.Choices, err)
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 2}) {
		t.Fatalf("clamped choices = %v, want [0 2]", in.Choices)
	}
}

// TestDigBudgetArmUnchangedWithoutMaxSum: a budget-less Dig keeps the plain
// first-Max policy byte-for-byte, so every existing bot-answered game is
// unaffected by this change.
func TestDigBudgetArmUnchangedWithoutMaxSum(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind: decision.KChoose,
		Min:  0,
		Max:  3,
		Options: []decision.Option{
			{Index: 0, Kind: "dig", Obj: 1},
			{Index: 1, Kind: "dig", Obj: 2},
			{Index: 2, Kind: "dig", Obj: 3},
		},
	}
	in := Decide(b, &d, rng(1))
	if !reflect.DeepEqual(in.Choices, []int{0, 1, 2}) {
		t.Fatalf("choices = %v, want all three (first-Max policy unchanged)", in.Choices)
	}
}
