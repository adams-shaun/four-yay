package searchbench

import (
	"math"
	"testing"
)

func TestScoreAlwaysPassiveIsBalancedChance(t *testing.T) {
	rows := []Decision{
		{ID: "1", Type: DecisionSpell, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{9}},
		{ID: "2", Type: DecisionHold, Human: Label{Alternatives: [][]int{{1}}}, AgentChoices: []int{9}},
		{ID: "3", Type: DecisionAttack, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{9}},
		{ID: "4", Type: DecisionAttack, Human: Label{Alternatives: [][]int{{1}}}, AgentChoices: []int{9}},
		{ID: "5", Type: DecisionBlock, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{9}},
		{ID: "6", Type: DecisionBlock, Human: Label{Alternatives: [][]int{{1}}}, AgentChoices: []int{9}},
	}
	s, err := Score(rows)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := s.BalancedAgreement()
	if !ok || math.Abs(got-.5) > 1e-12 {
		t.Fatalf("balanced=%v ok=%v, want .5", got, ok)
	}
}

func TestScoreKeepsAllowedSpellAlternativesSeparateFromActionMetric(t *testing.T) {
	s, err := Score([]Decision{
		{ID: "spell", Type: DecisionSpell, Human: Label{Alternatives: [][]int{{2}, {4}}, Act: true}, AgentChoices: []int{4}, AgentAct: false},
		{ID: "hold", Type: DecisionHold, Human: Label{Alternatives: [][]int{{4}}}, AgentChoices: []int{4}},
		{ID: "attack-yes", Type: DecisionAttack, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{1}, AgentAct: true},
		{ID: "attack-no", Type: DecisionAttack, Human: Label{Alternatives: [][]int{{2}}}, AgentChoices: []int{2}},
		{ID: "block-yes", Type: DecisionBlock, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{1}, AgentAct: true},
		{ID: "block-no", Type: DecisionBlock, Human: Label{Alternatives: [][]int{{2}}}, AgentChoices: []int{2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Matches[0] != 1 || s.WhichCount[0] != 0 {
		t.Fatalf("spell agreement/which = %d/%d", s.Matches[0], s.WhichCount[0])
	}
	if got, ok := s.CastHold.BalancedAccuracy(); !ok || got != .5 {
		t.Fatalf("cast/hold=%v ok=%v, want .5: %+v", got, ok, s.CastHold)
	}
	if got, ok := s.MacroAgreement(); !ok || got != 1 {
		t.Fatalf("macro=%v ok=%v, want 1", got, ok)
	}
}

func TestScoreRejectsNonCanonicalInput(t *testing.T) {
	_, err := Score([]Decision{{ID: "x", Type: DecisionSpell, Human: Label{Alternatives: [][]int{{1}}, Act: true}, AgentChoices: []int{2, 1}}})
	if err == nil {
		t.Fatal("Score accepted unsorted agent choice")
	}
}
