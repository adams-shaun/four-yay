package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func TestRemoveCreatureKeepsSubtypeOwnedByKindred(t *testing.T) {
	for _, tc := range []struct {
		name  string
		types string
	}{
		{name: "Kindred and Creature", types: "Kindred Creature Goblin"},
		{name: "Kindred only", types: "Kindred Goblin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			fixture := card(t, "Name:Kindred Type Strip\nTypes:"+tc.types+"\n"+
				"S:Mode$ Continuous | Affected$ Card.Self | RemoveType$ Creature\nOracle:x\n")
			id := onBoardCard(t, e, 0, fixture)
			if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
				t.Fatalf("precondition broken: fixture zone = %v, want ZBattlefield", got)
			}
			printed := e.G.Obj(id).Face().Types
			if !hasTypeWord(printed, "Kindred") || !hasTypeWord(printed, "Goblin") {
				t.Fatalf("precondition broken: printed types = %v, want Kindred Goblin", printed)
			}
			if tc.name == "Kindred and Creature" && !hasTypeWord(printed, "Creature") {
				t.Fatalf("precondition broken: printed types = %v, want Creature", printed)
			}

			derived := e.Derived(id).Types
			if hasTypeWord(derived, "Creature") {
				t.Fatalf("RemoveType$ Creature left Creature: %v", derived)
			}
			if !hasTypeWord(derived, "Kindred") || !hasTypeWord(derived, "Goblin") {
				t.Fatalf("removing Creature stripped the surviving Kindred subtype: %v", derived)
			}
		})
	}
}

func TestRemoveCreatureKeepsKindredSubtypeWithCorpusUniverse(t *testing.T) {
	universeFace := card(t, "Name:Kindred Artifact Creature Goblin\nTypes:Kindred Artifact Creature Goblin\nOracle:x\n")
	universe := []*cards.Card{universeFace}
	if len(universe) == 0 || !hasTypeWord(universe[0].Faces[0].Types, "Goblin") || !hasTypeWord(universe[0].Faces[0].Types, "Kindred") {
		t.Fatal("precondition broken: NameUniverse must contain a Kindred Artifact Creature Goblin face")
	}

	for _, tc := range []struct {
		name     string
		typeWord string
	}{
		{name: "corpus-owned subtype", typeWord: "Goblin"},
		{name: "Kindred-only corpus subtype fallback", typeWord: "Advisor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := []string{"Kindred", "Artifact", "Creature", tc.typeWord}
			if !effects.CreatureTypeWords(tc.typeWord) {
				t.Fatalf("precondition broken: %q is not recognized as a creature subtype", tc.typeWord)
			}
			if removedSubtype(tc.typeWord, "Creature", current, universe) {
				t.Fatalf("removing Creature stripped %s while Kindred remained (universe has %d faces)", tc.typeWord, len(universe))
			}
		})
	}
}
