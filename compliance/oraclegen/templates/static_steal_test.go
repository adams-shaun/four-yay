package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticYouDontOwn: Laughing Jasper Flint's "creatures you control but
// don't own are Mercenaries" (Affected$ Creature.YouCtrl+YouDontOwn) was
// unobservable because setup places nothing under another player's control.
// The fixture casts Mind Control on p1's probe after the card is in play. The
// precondition asserts a Bear really is controlled by p0 and owned by p1, that
// p0's own Bear is not a Mercenary, and that the Bear was not a Mercenary
// before the steal (it is not printed as one).
func TestStaticYouDontOwn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Laughing Jasper Flint"
	probe, _ := reg.Lookup("Grizzly Bears")
	if slices.Contains(probe.Faces[0].Types, "Mercenary") {
		t.Fatalf("precondition: Grizzly Bears is printed as a Mercenary")
	}
	_, final := selfFilterItem(t, reg, name, "static#0.0")
	stolen, own := 0, 0
	for _, p := range final.Permanents {
		if p.Name != "Grizzly Bears" || p.Controller != 0 {
			continue
		}
		if p.Owner == 1 {
			stolen++
			if !slices.Contains(p.Types, "Mercenary") {
				t.Fatalf("stolen Bear types = %v, want Mercenary added", p.Types)
			}
		} else {
			own++
			if slices.Contains(p.Types, "Mercenary") {
				t.Fatalf("p0's own Bear types = %v, want no Mercenary", p.Types)
			}
		}
	}
	if stolen != 1 || own != 1 {
		t.Fatalf("p0 controls %d stolen and %d own Bears, want 1 and 1: %+v", stolen, own, final.Permanents)
	}
}
