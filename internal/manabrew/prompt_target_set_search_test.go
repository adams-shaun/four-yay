//go:build manabrew

package manabrew

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

// TestConstrainedTargetPrefixControllerAndProperty pins the round-4 MAJOR: a
// TargetsWithSameController ask that ALSO carries SetPropShared. Hoisting the
// largest controller group and then the largest shared token picks one anchor,
// and neither anchor is guaranteed to be the controller that can fill Min
// while sharing a property. The engine still poses the ask because controller
// 0's two options share X, so the prefix must be that pair.
//
// Precondition: the naive two-hoist + greedyPrefix path really does fail
// (returns nil), so the assertion below genuinely depends on the fallback
// search, not on the offered order happening to work.
func TestConstrainedTargetPrefixControllerAndProperty(t *testing.T) {
	d := &decision.Decision{Seq: 11, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two", TargetsWithSameController: true, SetPropMode: decision.SetPropShared,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "c1 X", Obj: 60, Controller: 1, SetProps: []string{"X"}},
			{Index: 1, Kind: "permanent", Label: "c1 Y", Obj: 61, Controller: 1, SetProps: []string{"Y"}},
			{Index: 2, Kind: "permanent", Label: "c0 X", Obj: 62, Controller: 0, SetProps: []string{"X"}},
			{Index: 3, Kind: "permanent", Label: "c0 X", Obj: 63, Controller: 0, SetProps: []string{"X"}},
		}}
	// Precondition: the hoist-then-greedy path misses Min.
	naive := greedyPrefix(hoistByController(hoistBySharedToken(d.Options)), d)
	if naive != nil {
		t.Fatalf("fixture does not exercise the fallback: naive hoist already fills Min with %v", naive)
	}
	in := targetPrefix(t, d)
	if !strings.Contains(in.Presentation.Description, "All chosen targets must share one controller.") ||
		!strings.Contains(in.Presentation.Description, "All chosen targets must share a property.") {
		t.Fatalf("both constraint sentences must be present: %q", in.Presentation.Description)
	}
	got := prefixRefIDs(in, 2)
	if got[0] != "o62" || got[1] != "o63" {
		t.Fatalf("prefix candidates = %v, want controller 0's X-sharing pair [o62 o63]", got)
	}
	// The prefix answer validates under the decision's own validator.
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o62"}, {Kind: mb.RefCard, ID: "o63"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
	// A mixed-controller or non-sharing set is still fenced by Validate.
	for _, bad := range []decision.Intent{
		{Seq: d.Seq, Player: d.Player, Choices: []int{0, 2}}, // mixed controller
		{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}, // c1, X and Y
	} {
		if err := d.Validate(bad); err == nil {
			t.Fatalf("expected choices %v to be rejected", bad.Choices)
		}
	}
}

// TestConstrainedTargetPrefixDistinctPropertyFallback pins the structural
// half of the same MAJOR: SetPropDistinct without a controller constraint,
// where a greedy pass takes an early option whose property blocks every later
// one. A legal pair still exists, so the fallback search must find it.
//
// Precondition: greedyPrefix on the offered order really does fail.
func TestConstrainedTargetPrefixDistinctPropertyFallback(t *testing.T) {
	d := &decision.Decision{Seq: 12, Player: 1, Kind: decision.KTarget, Min: 2, Max: 2,
		Prompt: "Choose two differing", SetPropMode: decision.SetPropDistinct,
		Options: []decision.Option{
			{Index: 0, Kind: "permanent", Label: "AX", Obj: 70, SetProps: []string{"A", "X"}},
			{Index: 1, Kind: "permanent", Label: "A", Obj: 71, SetProps: []string{"A"}},
			{Index: 2, Kind: "permanent", Label: "X", Obj: 72, SetProps: []string{"X"}},
		}}
	if greedyPrefix(d.Options, d) != nil {
		t.Fatal("fixture does not exercise the fallback: greedy already finds a disjoint pair")
	}
	in := targetPrefix(t, d)
	got := prefixRefIDs(in, 2)
	if got[0] != "o71" || got[1] != "o72" {
		t.Fatalf("prefix candidates = %v, want the disjoint pair [o71 o72]", got)
	}
	p := pendingFor(d, battleView())
	intent := mustIntent(t, New("table", 2, nil).TranslateResponse(
		respFor(p, mb.BoardTargetsDecision{Chosen: []mb.TargetRef{
			{Kind: mb.RefCard, ID: "o71"}, {Kind: mb.RefCard, ID: "o72"},
		}}), p, d.Player))
	if err := d.Validate(*intent); err != nil {
		t.Fatalf("prefix answer rejected by the decision's own validator: %v", err)
	}
}
