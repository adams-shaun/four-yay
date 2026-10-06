package main

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestForgeAbilityLinesUseSetupFace(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Ojer Taq, Deepest Foundation"
	card, ok := reg.Lookup(name)
	if !ok || len(card.Faces) < 2 {
		t.Fatalf("%s missing from corpus or does not have a back face", name)
	}
	back := card.Faces[1]
	abilityIndex := -1
	for i, ability := range back.Abilities {
		if ability != nil {
			abilityIndex = i
			break
		}
	}
	if abilityIndex < 0 {
		t.Fatalf("%s back face has no non-nil ability", name)
	}

	idx := abilityIndex
	backItem := oraclegen.Item{Scenario: oraclegen.Scenario{
		Setup: map[string]oraclegen.Seat{"p0": {BackFace: []string{name}}},
		Steps: []oraclegen.Step{{Op: "activate", Card: "p0:" + name, AbilityIndex: &idx}},
	}}
	backLines := forgeAbilityLines(reg, backItem)
	gotBack, exists := backLines["0"]
	if !exists || gotBack != back.Abilities[abilityIndex].Line {
		t.Fatalf("back-face ability line = %q (present %v), want %q", gotBack, exists, back.Abilities[abilityIndex].Line)
	}

	frontFace := card.Faces[0]
	frontItem := backItem
	frontItem.Scenario.Setup = map[string]oraclegen.Seat{"p0": {}}
	frontLines := forgeAbilityLines(reg, frontItem)
	frontIndex := -1
	for i, ability := range frontFace.Abilities {
		if ability != nil {
			frontIndex = i
			break
		}
	}
	if frontIndex < 0 {
		if _, exists := frontLines["0"]; exists {
			t.Errorf("front face has no ability but exported %q", frontLines["0"])
		}
		return
	}
	frontStep := frontItem.Steps[0]
	frontStep.AbilityIndex = &frontIndex
	frontItem.Steps = []oraclegen.Step{frontStep}
	frontLines = forgeAbilityLines(reg, frontItem)
	if got, want := frontLines["0"], frontFace.Abilities[frontIndex].Line; got != want {
		t.Errorf("front-face ability line = %q, want %q", got, want)
	}
}
