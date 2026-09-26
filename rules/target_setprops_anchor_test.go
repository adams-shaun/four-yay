package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
)

// This file pins the SHARED-class anchor repair in botpolicy.Clamp. The
// SetPropShared constraint seeds its running intersection from the first pick,
// so a pick alone in its property class makes every append refused and the
// ORDINARY top-up can only return an answer shorter than Min -- an answer
// Decision.Validate rejects and the deterministic bot re-derives forever.
// setPropSharedChoices re-anchors on each represented class before the
// ordinary repair, exactly as sameControllerChoices re-anchors on each
// represented controller. These tests drive the REAL bot entry points
// (Clamp and Decide), not a private helper, so the one-home contract is what
// is proven.

// loneClassDecision is the review probe's shape: Min 2, Max 2, three options
// whose property tokens are a, b, b. The pair {1,2} shares b and is legal;
// option 0 is alone in class a, so any answer beginning at 0 cannot reach two
// picks under the shared rule.
func loneClassDecision() *decision.Decision {
	return &decision.Decision{
		Seq: 9, Player: 0, Kind: decision.KTarget, Min: 2, Max: 2,
		SetPropMode: decision.SetPropShared,
		Options: []decision.Option{
			{Index: 0, SetProps: []string{"a"}},
			{Index: 1, SetProps: []string{"b"}},
			{Index: 2, SetProps: []string{"b"}},
		},
	}
}

// TestTargetSetSharedClampReanchorsLoneClass pins the MAJOR: a preferred first
// pick alone in its shared class must not leave Clamp returning a short answer
// Validate rejects. The precondition asserts the trap really exists -- option
// 0 shares its token with no other option, while {1,2} is a legal pair.
func TestTargetSetSharedClampReanchorsLoneClass(t *testing.T) {
	d := loneClassDecision()
	// Preconditions: the lone anchor really is alone, and {1,2} really is a
	// legal completion, so a correct repair has somewhere to land.
	if got := decision.SetPropCapacity(decision.SetPropShared, [][]string{{"a"}, {"b"}, {"b"}}); got != 2 {
		t.Fatalf("precondition: shared capacity = %d, want 2 ({1,2})", got)
	}
	if decision.SetPropAdmits(decision.SetPropShared, []string{"a"}, []string{"b"}) {
		t.Fatal("precondition: class a must not admit class b")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 2}}); err != nil {
		t.Fatalf("precondition: {1,2} must be a legal shared pair: %v", err)
	}
	// The input the bot's own pick loop produces: it leads with option 0.
	clamped := botpolicy.Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}})
	if err := d.Validate(clamped); err != nil {
		t.Fatalf("Clamp returned an invalid shared answer: %v (%+v)", err, clamped)
	}
	if len(clamped.Choices) != 2 {
		t.Fatalf("Clamp = %v, want two sharing picks", clamped.Choices)
	}
}

// TestTargetSetSharedDecideReanchorsLoneClass drives the whole bot entry
// point: boardpolicy.Decide on the same decision must return an answer
// Decision.Validate accepts, so the deterministic bot is not left
// re-deriving a rejected intent (the livelock class).
func TestTargetSetSharedDecideReanchorsLoneClass(t *testing.T) {
	d := loneClassDecision()
	// Precondition: the raw pick loop is offered this shape and there is a
	// legal completion, so the test cannot pass merely by accident.
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 2}}); err != nil {
		t.Fatalf("precondition: {1,2} must be a legal shared pair: %v", err)
	}
	got := botpolicy.Decide(botpolicy.Board{}, d, nil)
	if err := d.Validate(got); err != nil {
		t.Fatalf("Decide returned an invalid shared answer: %v (%+v)", err, got)
	}
	if len(got.Choices) != 2 {
		t.Fatalf("Decide = %v, want two sharing picks", got.Choices)
	}
}

// TestTargetSetSharedClampKeepsValidAnswer asserts the re-anchor is not a
// behaviour change for an already-valid answer: the clamp of a conforming
// input stays byte-identical, so no repo deck's bot pick moves.
func TestTargetSetSharedClampKeepsValidAnswer(t *testing.T) {
	d := loneClassDecision()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1, 2}}
	if err := d.Validate(in); err != nil {
		t.Fatalf("precondition: {1,2} must be a legal shared pair: %v", err)
	}
	got := botpolicy.Clamp(d, in)
	if len(got.Choices) != 2 || got.Choices[0] != 1 || got.Choices[1] != 2 {
		t.Fatalf("Clamp changed a valid shared answer: %v, want [1 2]", got.Choices)
	}
}
