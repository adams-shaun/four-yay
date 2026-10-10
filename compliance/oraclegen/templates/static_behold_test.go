package templates_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticBeholdProbe: Champion of the Clachan's "Other Kithkin you control
// get +1/+1" was unobservable because the Kithkin probe on the battlefield is
// itself a legal choice for the card's own "behold a Kithkin and exile it"
// cast cost, and the cast exiled it before the checkpoint. The fixture holds a
// spare Kithkin in hand and scripts the cast's behold pick to it. The
// precondition asserts the probe is on p0's battlefield at the final
// checkpoint (not exiled), the spare is what was exiled, and the probe's P/T
// is the printed one raised by +1/+1.
func TestStaticBeholdProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name, probe = "Champion of the Clachan", "Eclipsed Kithkin"
	pc, ok := reg.Lookup(probe)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", probe)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.1")
	if !slices.Contains(it.Setup["p0"].Battlefield, probe) || !slices.Contains(it.Setup["p0"].Hand, "Kithkin Greatheart") {
		t.Fatalf("setup p0 = %+v, want the probe on the battlefield and the spare Kithkin in hand", it.Setup["p0"])
	}
	if !slices.Contains(final.Players[0].Exile, "Kithkin Greatheart") || slices.Contains(final.Players[0].Exile, probe) {
		t.Fatalf("p0 exile = %v, want the spare exiled to pay and the probe kept", final.Players[0].Exile)
	}
	scripted := false
	for _, step := range it.XAnswers {
		for _, a := range step {
			if a.Value == "Kithkin Greatheart" {
				scripted = true
			}
		}
	}
	if !scripted {
		t.Fatalf("XMage answers = %+v, want the behold pick Kithkin Greatheart queued", it.XAnswers)
	}
	p, ok := permNamedSnap(final, probe)
	if !ok {
		t.Fatalf("%s not on the final battlefield", probe)
	}
	// Printed P/T "a/b" reads (a+1)/(b+1) under the static.
	var a, b int
	if n, err := fmt.Sscanf(pc.Faces[0].PT, "%d/%d", &a, &b); err != nil || n != 2 {
		t.Fatalf("precondition: %s printed P/T %q: %v", probe, pc.Faces[0].PT, err)
	}
	if want := fmt.Sprintf("%d/%d", a+1, b+1); p.PT != want {
		t.Fatalf("%s P/T = %s, want %s (printed %s plus +1/+1)", probe, p.PT, want, pc.Faces[0].PT)
	}
}
