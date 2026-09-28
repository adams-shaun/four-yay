package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestLegacyETBNameOptionsKeepThePreUniverseBuilder pins the configuration a
// sidecar from before NameUniverse replays with. Pithing Needle's absent
// ValidCards$ used to default to nonland over visible objects, while Alpine
// Moon's nontrivial filter selected only visible nonbasic lands. These are
// intentionally NOT the modern corpus-universe rules; changing either list
// changes the recorded DecisionAsk of an old match.
func TestLegacyETBNameOptionsKeepThePreUniverseBuilder(t *testing.T) {
	t.Parallel()
	e, _ := nameCardEngine(t, "Pithing Needle", "Alpine Moon", "Wasteland", "Forest")
	// Precondition: this test must exercise a no-universe replay mode, not
	// the full-corpus modern path nameCardEngine normally wires.
	e.G.NameUniverse = nil
	e.G.NameUniverseNames = nil

	needle := e.G.Zone(state.ZHand, 0)[0]
	needleOptions := e.etbOptions(0, needle, "name", "", "", "", "")
	if hasNameOption(needleOptions, "Wasteland") {
		t.Fatal("legacy Pithing Needle offered Wasteland; its empty ValidCards$ must retain the old nonland default")
	}
	if !hasNameOption(needleOptions, "Pithing Needle") {
		t.Fatalf("legacy Pithing Needle options %v omit its visible nonland candidate", needleOptions)
	}

	moon := e.G.Zone(state.ZHand, 0)[1]
	moonOptions := e.etbOptions(0, moon, "name", "Card.Land+nonBasic", "", "", "")
	if !hasNameOption(moonOptions, "Wasteland") {
		t.Fatalf("legacy Alpine Moon options %v omit visible nonbasic land Wasteland", moonOptions)
	}
	if hasNameOption(moonOptions, "Forest") {
		t.Fatal("legacy Alpine Moon offered basic Forest; its full ValidCards$ filter was not applied")
	}
}

func hasNameOption(options []decision.Option, name string) bool {
	for _, option := range options {
		if option.Label == name {
			return true
		}
	}
	return false
}
