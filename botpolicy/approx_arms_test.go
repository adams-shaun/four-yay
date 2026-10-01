package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the real arms the approx tickets added to Decide for
// decisions that previously fell through to Clamp's option-0 fallback: the
// CR 616.1 KReplacement order choice, the CR 702.43 multikicker count ask and
// the CR 702.140b mutate over/under placement ask. Each test asserts its own
// precondition (the metric the arm ranks on actually differs, and every
// option the arm must choose between is present) so a vacuous setup fails
// loudly instead of passing silently.
//
// The later replicate/squad widening of the same count case is pinned by
// approx_count_arms_test.go (CR 702.55a and CR 702.66), kept in its own file
// under the 2026-09-22 "new tests go in a new file" rule.

// TestBotReplacementOrderRanksBySourceWorth is (a): the order arm picks the
// replacement whose source permanent has the higher cardWorth, not the option
// that happens to be offered first.
func TestBotReplacementOrderRanksBySourceWorth(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{
		10: {Creature: true, Power: 5}, // worth 30 + 4*5 = 50
		20: {Creature: true, Power: 1}, // worth 30 + 4*1 = 34
	})}
	// Precondition: the arm has a real ranking to make.
	hi, lo := b.cardWorth(10), b.cardWorth(20)
	if hi == lo {
		t.Fatalf("test setup: the two sources have equal worth %d, so the ranking cannot be observed", hi)
	}
	if hi < lo {
		t.Fatalf("test setup: source 10 worth %d is not above source 20 worth %d", hi, lo)
	}
	d := &decision.Decision{Player: 0, Kind: decision.KReplacement, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "replacement", Obj: 20, Label: "low worth"},
			{Index: 1, Kind: "replacement", Obj: 10, Label: "high worth"},
		}}
	in := Decide(b, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("replacement order pick = %v, want [1] (the higher-worth source)", in.Choices)
	}
}

// TestBotReplacementOrderBypassesSkip is (a)'s opt-out half: a
// "skip_replacement" option never wins while a real replacement is offered.
func TestBotReplacementOrderBypassesSkip(t *testing.T) {
	b := Board{Cards: TableOf(map[state.ObjID]Card{10: {Creature: true, Power: 3}})}
	d := &decision.Decision{Player: 0, Kind: decision.KReplacement, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "skip_replacement", Label: "Do not apply an optional replacement"},
			{Index: 1, Kind: "replacement", Obj: 10, Label: "Apply a replacement"},
		}}
	// Precondition: both shapes are actually on the list under test.
	if d.Options[0].Kind != "skip_replacement" || d.Options[1].Kind != "replacement" {
		t.Fatalf("test setup: option kinds are %q/%q", d.Options[0].Kind, d.Options[1].Kind)
	}
	in := Decide(b, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("skip bypass pick = %v, want [1] (the real replacement)", in.Choices)
	}
}

// TestBotMultikickPaysTheAffordableMaximum is (b): option 0 is "No multikick"
// (Amount 0); the arm pays the largest affordable count instead.
func TestBotMultikickPaysTheAffordableMaximum(t *testing.T) {
	d := &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "multikick", Label: "No multikick", Amount: 0},
			{Index: 1, Kind: "multikick", Label: "Pay multikicker once", Amount: 1},
			{Index: 2, Kind: "multikick", Label: "Pay multikicker 2 times", Amount: 2},
		}}
	// Precondition: the decline differs from the maximum the arm must reach.
	if d.Options[0].Amount != 0 || d.Options[len(d.Options)-1].Amount != 2 {
		t.Fatalf("test setup: amounts are %d..%d", d.Options[0].Amount, d.Options[len(d.Options)-1].Amount)
	}
	in := Decide(Board{}, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 2 {
		t.Fatalf("multikick pick = %v, want [2] (the maximum affordable count)", in.Choices)
	}
}

// TestBotMutatePlacesUnder is (c): option 0 is "On top"; the arm places the
// mutating card under the target so the target's characteristics survive.
func TestBotMutatePlacesUnder(t *testing.T) {
	d := &decision.Decision{Player: 0, Kind: decision.KChoose, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "mutate_place", Label: "On top", Amount: 1},
			{Index: 1, Kind: "mutate_place", Label: "Under", Amount: 0},
		}}
	// Precondition: both placement options are present and differ, and the
	// engine's option 0 is the on-top option the fallback would take.
	if d.Options[0].Label != "On top" || d.Options[1].Label != "Under" {
		t.Fatalf("test setup: labels are %q/%q", d.Options[0].Label, d.Options[1].Label)
	}
	if d.Options[0].Amount == d.Options[1].Amount {
		t.Fatalf("test setup: both placements carry Amount %d", d.Options[0].Amount)
	}
	in := Decide(Board{}, d, rng(1))
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("mutate placement pick = %v, want [1] (Under)", in.Choices)
	}
}
