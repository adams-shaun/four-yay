package rules

import (
	"slices"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A registration covers the API name but must not certify its unimplemented
// selectors. Pin both sides of the class so a new carrier is investigated.
func TestMustBlockNamedTargetShapeCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var supported, other []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			p := f.Primitives()
			if !slices.Contains(p, "api:MustBlock") {
				continue
			}
			if slices.Contains(p, "api:MustBlock.OtherShape") {
				other = append(other, f.Name)
			} else {
				supported = append(supported, f.Name)
			}
		}
	}
	sort.Strings(supported)
	sort.Strings(other)
	wantSupported := []string{"Auriok Siege Sled", "Avalanche Tusker", "Blaze of Glory", "Burning-Tree Bloodscale", "Crashing Boars", "Feral Contest", "Fighter Class", "Giant Ambush Beetle", "Grappling Hook", "Hunt Down", "Impetuous Devils", "Lineprancers", "Lurking Arynx", "Magnetic Web", "Maraleaf Rider", "Matsu-Tribe Decoy", "Monstrous Step", "Rampant Elephant", "Rimehorn Aurochs", "Sisters of Stone Death", "Tangle Angler", "Tolsimir, Midnight's Light", "Torchling", "Tower Above", "Trumpeting Armodon", "Turntimber Basilisk", "Vortex Elemental"}
	if !slices.Equal(supported, wantSupported) {
		t.Errorf("supported MustBlock selector carriers: %q; want %q", supported, wantSupported)
	}
	var wantOther []string
	if !slices.Equal(other, wantOther) {
		t.Errorf("unsupported MustBlock selector carriers: %q; want %q", other, wantOther)
	}
}
