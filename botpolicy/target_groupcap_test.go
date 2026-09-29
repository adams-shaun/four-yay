package botpolicy

// Test for target.go's KTarget pick loop deriving its group discipline from
// decision.GroupAdmits (groupcap-wording): the old loop re-implemented the
// cap-1 exclusivity rule with a parallel `chosen map[string]bool`, so a
// decision whose GroupLimit raises a group's cap above 1 would leave the
// legal second same-group option unpicked -- on a Min-2 ask whose top-ranked
// pair shares the group, the answer comes back SHORT and the bot is re-asked
// forever (the livelock shape). At cap 1 the two forms are equivalent, which
// TestTargetNeverTakesTwoOfOneGroup keeps pinning unchanged.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestTargetGroupCapRaisedPicksBothOfOneGroup(t *testing.T) {
	b := boardOf(def(1, 5, 5), def(2, 5, 5), def(3, 1, 1))
	opts := []decision.Option{
		{Index: 0, Kind: "permanent", Obj: 201, Player: 1, Group: "g"},
		{Index: 1, Kind: "permanent", Obj: 202, Player: 1, Group: "g"},
		{Index: 2, Kind: "permanent", Obj: 203, Player: 2, Group: "h"},
	}
	d := &decision.Decision{Seq: 41, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		GroupLimit: 2, Options: opts}
	// Preconditions: the cap is real and drives the difference -- the cap-1
	// twin refuses the same-group pair the raised-cap decision must admit,
	// and the twin's own pick (one of each group) differs from the pair.
	twin := *d
	twin.GroupLimit = 0
	if err := twin.Validate(decision.Intent{Seq: twin.Seq, Player: twin.Player, Choices: []int{0, 1}}); err == nil {
		t.Fatal("precondition: the cap-1 twin accepted the same-group pair {0,1}")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatalf("the raised-cap decision refused the pair it must admit: %v", err)
	}
	twinPick := b.chooseTargets(&twin)
	if len(twinPick) != 2 || opts[twinPick[0]].Group == opts[twinPick[1]].Group {
		t.Fatalf("precondition: cap-1 twin pick = %v, want two options of DIFFERENT groups", twinPick)
	}
	ch := b.chooseTargets(d)
	if len(ch) != 2 {
		t.Fatalf("chooseTargets(Min 2) = %v, want two choices", ch)
	}
	if ch[0] != 0 || ch[1] != 1 {
		t.Fatalf("chooseTargets = %v, want BOTH same-group options [0 1] at the raised cap", ch)
	}
	// The pick loop's answer must be one the engine's own validator accepts
	// (the one-home invariant: policy and Validate cannot drift).
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
		t.Fatalf("the pick loop's answer failed the decision's own validator: %v", err)
	}
}
