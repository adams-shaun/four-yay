package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// A per-alternative zone is not a reason to replace a type already served by
// the historical graveyard fixture with the registry's first matching card.
func TestExplicitMixedZonePreservesLegacyCreature(t *testing.T) {
	reg := loadGenRegistry(t)
	filter := "Creature.inZoneGraveyard+YouCtrl,Instant.inZoneExile+YouCtrl@Graveyard,Exile"
	fxs := oraclegen.Fixtures(reg, []oraclegen.SlotSpec{{Filter: filter}})
	if len(fxs) == 0 || len(fxs[0].Targets()) != 1 {
		t.Fatalf("%s: missing one target: %v", filter, fxs)
	}
	// Preconditions: the historical card is a creature, and the filter
	// really does ask for a graveyard creature and an exile instant.
	if !faceHasType(t, reg, "Grizzly Bears", "Creature") || !strings.Contains(filter, "Instant.inZoneExile") {
		t.Fatal("mixed-zone filter or legacy creature is not as expected")
	}
	if got := fxs[0].Targets()[0]; got != "p0:Grizzly Bears" || !containsName(fxs[0].P0().Graveyard, "Grizzly Bears") {
		t.Fatalf("explicit mixed-zone first target = %q, graveyard %v; want legacy p0:Grizzly Bears", got, fxs[0].P0().Graveyard)
	}
}
