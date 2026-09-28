package rules

import (
	"testing"

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
