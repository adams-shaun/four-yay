package mbtest

import (
	"strconv"
	"testing"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

func powerCard(id, power string) mb.CardDto {
	p := power
	return mb.CardDto{ID: id, Power: &p}
}

func chosenIDs(t *testing.T, out mb.PromptOutputValue) []string {
	t.Helper()
	dec, ok := out.(mb.ChooseCardsDecision)
	if !ok {
		t.Fatalf("answer type %T, want ChooseCardsDecision", out)
	}
	return dec.ChosenCardIDs
}

// TestMockChooseCardsHonoursFloorAndCeilingTogether pins the round-4 MAJOR:
// a choose-cards ask whose prompt carries BOTH a value floor and a value
// ceiling must return a set satisfying both. With Min=Max=1, floor 5 and
// ceiling 6, the strongest-first floor pick (power 10) overshoots the
// ceiling; the legal answer is the power-5 card.
//
// Preconditions: the strongest card really does exceed the ceiling and the
// weaker one really does lie inside the bounds, so a fix that ignores either
// bound cannot pass.
func TestMockChooseCardsHonoursFloorAndCeilingTogether(t *testing.T) {
	in := mb.ChooseCardsInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{
			Title:       "Choose 1 to 1.",
			Description: "Choose 1 to 1. Total value must not exceed 6. Total value must be at least 5.",
		}},
		Cards: []mb.CardDto{powerCard("c10", "10"), powerCard("c5", "5")},
		Min:   1, Max: 1,
	}
	if sumOf([]string{"c10"}, in.Cards) != 10 {
		t.Fatal("fixture: c10 does not carry power 10")
	}
	if sumOf([]string{"c5"}, in.Cards) != 5 {
		t.Fatal("fixture: c5 does not carry power 5")
	}
	out := NewFirstLegalClient().answerChooseCards(in)
	got := chosenIDs(t, out)
	if len(got) != 1 || got[0] != "c5" {
		t.Fatalf("chosen = %v, want the ceiling-respecting [c5]", got)
	}
	sum := sumOf(got, in.Cards)
	if sum < 5 || sum > 6 {
		t.Fatalf("chosen sum = %d, want within [5,6]", sum)
	}
}

// TestMockChooseCardsCeilingOnlyStillPicksCheap verifies the ceiling-only
// path is unchanged by the combined search: the weakest card is chosen.
func TestMockChooseCardsCeilingOnlyStillPicksCheap(t *testing.T) {
	in := mb.ChooseCardsInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{
			Title:       "Choose 1 to 1.",
			Description: "Choose 1 to 1. Total value must not exceed 3.",
		}},
		Cards: []mb.CardDto{powerCard("c10", "10"), powerCard("c2", "2")},
		Min:   1, Max: 1,
	}
	got := chosenIDs(t, NewFirstLegalClient().answerChooseCards(in))
	if len(got) != 1 || got[0] != "c2" {
		t.Fatalf("chosen = %v, want the cheapest [c2]", got)
	}
}

// TestMockChooseCardsFloorOnlyReachesFloor verifies the floor-only path:
// with Min 1 the strongest card reaches the floor immediately.
func TestMockChooseCardsFloorOnlyReachesFloor(t *testing.T) {
	in := mb.ChooseCardsInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{
			Title:       "Choose 1 to 2.",
			Description: "Choose 1 to 2. Total value must be at least 5.",
		}},
		Cards: []mb.CardDto{powerCard("c10", "10"), powerCard("c2", "2")},
		Min:   1, Max: 2,
	}
	got := chosenIDs(t, NewFirstLegalClient().answerChooseCards(in))
	if len(got) != 1 || got[0] != "c10" {
		t.Fatalf("chosen = %v, want the strongest single card [c10]", got)
	}
}

// TestMockChooseCardsZeroCeilingIsHonoured pins the second half of the same
// class: a present "must not exceed 0." sentence is a real bound
// (Decision.Budgeted with MaxSum 0), not an absent one. With Min=1 over a +1
// and a -1 card, the +1 card alone breaks the zero ceiling while the -1 card
// obeys it; a mock that drops the bound takes the +1 card (offered first).
func TestMockChooseCardsZeroCeilingIsHonoured(t *testing.T) {
	in := mb.ChooseCardsInput{
		PromptBase: mb.PromptBase{Presentation: mb.PromptPresentation{
			Title:       "Choose 1 to 2.",
			Description: "Choose 1 to 2. Total value must not exceed 0.",
		}},
		Cards: []mb.CardDto{powerCard("up", "1"), powerCard("down", "-1")},
		Min:   1, Max: 2,
	}
	got := chosenIDs(t, NewFirstLegalClient().answerChooseCards(in))
	if len(got) != 1 || got[0] != "down" {
		t.Fatalf("chosen = %v, want the ceiling-respecting [down]", got)
	}
	if sum := sumOf(got, in.Cards); sum > 0 {
		t.Fatalf("chosen sum = %d, want <= 0", sum)
	}
}

// sumOf returns the power sum of the named ids over cards, preserving the
// input id order; a missing or unparseable id contributes 0.
func sumOf(ids []string, cards []mb.CardDto) int {
	total := 0
	for _, id := range ids {
		for _, c := range cards {
			if c.ID == id && c.Power != nil {
				if n, err := strconv.Atoi(*c.Power); err == nil {
					total += n
				}
			}
		}
	}
	return total
}
