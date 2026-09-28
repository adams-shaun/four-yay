package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestETBRootsOfLifeHonoursInvalidBasicLandTypes exercises the corpus card's
// actual as-enters SVar through the ETB choice path. Its InvalidTypes$ must
// leave only Island and Swamp offerable.
func TestETBRootsOfLifeHonoursInvalidBasicLandTypes(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Roots of Life"))
	id := e.G.Zone(state.ZHand, 0)[0]
	labels := etbTypeChoiceLabels(t, e, id)
	if len(labels) != 2 || labels[0] != "Island" || labels[1] != "Swamp" {
		t.Fatalf("Roots of Life entry options = %v, want [Island Swamp]", labels)
	}
}
