package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticChosenType: Leyline of Transformation's "creatures you control are
// the chosen type in addition to their other types" (AddType$ ChosenType) was
// unobservable because gorge's creature-type ask leads with the types the
// chooser owns, so the unscripted pick is the probe's own Bear subtype and the
// added type changes nothing. The fixture scripts the as-enters ask to a type
// the probe lacks, and the same answer reaches XMage's choice queue. The
// precondition asserts the probe is a Bear that is not a Wizard, the opponent's
// probe (not under the Leyline) stays unchanged, and the scripted answer is
// the one the replay actually recorded.
func TestStaticChosenType(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Leyline of Transformation"
	const chosen = "Wizard"
	probe, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatalf("precondition: Grizzly Bears not in the corpus")
	}
	if !slices.Contains(probe.Faces[0].Types, "Bear") || slices.Contains(probe.Faces[0].Types, chosen) {
		t.Fatalf("precondition: probe types = %v, want a Bear that is not a %s", probe.Faces[0].Types, chosen)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	var mine, theirs []string
	for _, p := range final.Permanents {
		if p.Name != "Grizzly Bears" {
			continue
		}
		if p.Controller == 0 {
			mine = p.Types
		} else {
			theirs = p.Types
		}
	}
	if !slices.Contains(mine, chosen) || !slices.Contains(mine, "Bear") {
		t.Fatalf("p0's Bear types = %v, want Bear and the chosen %s", mine, chosen)
	}
	if slices.Contains(theirs, chosen) {
		t.Fatalf("p1's Bear types = %v, want no %s (the Leyline's controller only)", theirs, chosen)
	}
	found := false
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Kind == "choice" && a.Value == chosen {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("XMage answers = %+v, want the chosen type %s queued", it.XAnswers, chosen)
	}
}
