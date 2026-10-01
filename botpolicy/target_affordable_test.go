package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// strive1: a Strive spell's "any number of targets" ask (Min 0) used to be
// answered at its full legal width, pricing one extra Strive payment per
// target the bot could not pay. The cast reversed (CR 733.1) and the bot
// re-proposed the identical unpayable cast (cardfuzz batch2 lines 19/22/30/
// 41/45: Phalanx Formation, Nature's Panoply, Hour of Need, Aerial Formation,
// Setessan Tactics). The engine now carries the largest affordable count on
// the ask (Decision.AffordableTargets); the bot must stay within it.
func TestTargetStaysWithinAffordableCount(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{})}
	opts := []decision.Option{
		{Index: 0, Kind: "permanent", Obj: 1},
		{Index: 1, Kind: "permanent", Obj: 2},
		{Index: 2, Kind: "permanent", Obj: 3},
		{Index: 3, Kind: "permanent", Obj: 4},
	}
	for _, tc := range []struct {
		affordable, want int
	}{
		{0, 4}, // no hint: the full legal width, unchanged
		{1, 1},
		{2, 2},
		{9, 4}, // a hint above Max never widens the pick
	} {
		d := decision.Decision{Player: 0, Kind: decision.KTarget, Min: 0, Max: 4,
			Options: opts, AffordableTargets: tc.affordable}
		in := Decide(b, &d, rng(1))
		if err := d.Validate(in); err != nil {
			t.Fatalf("affordable=%d: answer %v failed Validate: %v", tc.affordable, in.Choices, err)
		}
		if len(in.Choices) != tc.want {
			t.Fatalf("affordable=%d: picked %d targets %v, want %d", tc.affordable, len(in.Choices), in.Choices, tc.want)
		}
	}
	// The hint never drops the answer below the decision's own Min.
	d := decision.Decision{Player: 0, Kind: decision.KTarget, Min: 2, Max: 4,
		Options: opts, AffordableTargets: 1}
	in := Decide(b, &d, rng(1))
	if err := d.Validate(in); err != nil || len(in.Choices) != 2 {
		t.Fatalf("Min 2 with hint 1: answer %v (err %v), want the 2 Min demands", in.Choices, err)
	}
}
