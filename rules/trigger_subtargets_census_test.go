package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// triggerChainPreAskCarriers is every corpus card with a printed trigger whose
// chain triggerChainPreAsks announces at placement (CR 603.3d): a targeting
// SubAbility$ link followed later by an untargeted Defined$ Targeted link.
// Measured 2026-10-03 at FORGE_REF over every face's T: lines. The same walk
// over raw scripts finds one more chain of the family, Rhino, Terrible
// Trampler, whose tail reads Defined$ ParentTarget (the PARENT link's
// targets, not the union), so it stays out of scope.
var triggerChainPreAskCarriers = []string{
	"All Shall Smolder in My Wake",
	"Goblin Grenadiers",
	"Jon Irenicus, Shattered One",
	"Kimahri, Valiant Guardian",
	"The Spot, Living Portal",
	"Uldaros Theorix",
}

// TestTriggerChainPreAskCensus pins the scoped class both ways: a carrier
// that drifts out of the shape, or a new corpus chain that enters it, fails
// here rather than silently changing which triggers announce link targets
// at placement.
func TestTriggerChainPreAskCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := &Engine{}
	seen := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, tr := range f.Triggers {
				if len(e.triggerChainPreAsks(tr.Effect)) > 0 {
					seen[c.Faces[0].Name] = true
				}
			}
		}
	}
	got := make([]string, 0, len(seen))
	for name := range seen {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), triggerChainPreAskCarriers...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("placement chain pre-ask carriers:\n  got  %q\n  want %q", got, want)
	}
}
