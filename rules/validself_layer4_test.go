package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Count$ValidSelf Card$CreatureType is evaluated in layer 7c, but its count
// observes layer-4 characteristics rather than only the printed face.
func TestCountValidSelfCreatureTypesIncludesLayer4Grants(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := combatEngine(t)
	onBoardCard(t, e, 0, tlaCorpusCard(t, reg, "Diligent Zookeeper"))
	cat := onBoard(t, e, 0, "Name:Cat Soldier\nManaCost:0\nTypes:Creature Cat Soldier\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Bird Grant\nTypes:Creature Wizard\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddTypes$ Bird | Description$ x\nOracle:x\n")

	derived := e.Derived(cat)
	hasBird := false
	for _, typ := range derived.Types {
		if typ == "Bird" {
			hasBird = true
			break
		}
	}
	if !hasBird {
		t.Fatalf("precondition: layer-4 derived types %v do not contain Bird", derived.Types)
	}
	if derived.Power != 5 || derived.Toughness != 5 {
		t.Fatalf("Cat Soldier with layer-4 Bird grant P/T = %d/%d, want 5/5 (three effective creature types)", derived.Power, derived.Toughness)
	}
}
