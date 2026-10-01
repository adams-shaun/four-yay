package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// The budget arm for a KModes carrying a cumulative WithTotalCMC$ budget
// (Decision.MaxSum > 0) -- a Play grant (Invoke Calamity, Rod of Absorption,
// Primeval Spawn): a blind first-Min answer can exceed the sum cap,
// Decision.Validate rejects it, and the bot re-derives the same rejected
// answer forever. The arm fills greedily in offered order while the running
// Value sum fits -- the same fill the shared KChoose budget arm uses.

// TestPlayBudgetModesArmFillsGreedilyUnderMaxSum: a mandatory budget Play
// (Min 2, Max 2, MaxSum 5) over options priced [3,4,2] takes the 3 (fits)
// and then the 2 (3+2=5 fits), skipping the 4 -- a blind first-Min answer
// [0,1] sums to 7 and would be rejected -- and the answer passes
// Decision.Validate, so the engine consumes it and no livelock occurs.
func TestPlayBudgetModesArmFillsGreedilyUnderMaxSum(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:   decision.KModes,
		Min:    2,
		Max:    2,
		MaxSum: 5,
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Obj: 1, Value: 3},
			{Index: 1, Kind: "mode", Obj: 2, Value: 4},
			{Index: 2, Kind: "mode", Obj: 3, Value: 2},
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

// TestPlayBudgetModesOptionalDeclines: a Min-0 (Optional$) budget Play -- the
// shape every corpus WithTotalCMC$ Play carrier has -- is answered with the
// empty intent (the decline), which passes Validate.
func TestPlayBudgetModesOptionalDeclines(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	d := decision.Decision{
		Kind:   decision.KModes,
		Min:    0,
		Max:    2,
		MaxSum: 6,
		Options: []decision.Option{
			{Index: 0, Kind: "mode", Obj: 1, Value: 5},
			{Index: 1, Kind: "mode", Obj: 2, Value: 5},
		},
	}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("bot answer %v failed Validate: %v", in.Choices, err)
	}
	if len(in.Choices) != 0 {
		t.Fatalf("choices = %v, want the empty decline of an Optional$ budget Play", in.Choices)
	}
}
