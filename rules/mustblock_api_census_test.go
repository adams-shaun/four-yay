package rules

import (
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The API carriers have several target/attacker selector grammars; keep the
// complete class visible even when a newly pinned script adds another shape.
func TestMustBlockAPICarrierCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var got []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f != nil && slices.Contains(f.Primitives(), "api:MustBlock") {
				got = append(got, f.Name)
			}
		}
	}
	sort.Strings(got)
	want := []string{
		"Auriok Siege Sled", "Avalanche Tusker", "Blaze of Glory", "Burning-Tree Bloodscale", "Crashing Boars", "Feral Contest", "Fighter Class", "Giant Ambush Beetle", "Grappling Hook", "Hunt Down", "Impetuous Devils", "Lineprancers", "Lurking Arynx", "Magnetic Web", "Maraleaf Rider", "Matsu-Tribe Decoy", "Monstrous Step", "Rampant Elephant", "Rimehorn Aurochs", "Sisters of Stone Death", "Tangle Angler", "Tolsimir, Midnight's Light", "Torchling", "Tower Above", "Trumpeting Armodon", "Turntimber Basilisk", "Vortex Elemental",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("api:MustBlock carriers: got %q, want %q", got, want)
	}
}
