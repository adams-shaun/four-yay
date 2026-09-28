package rules

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSetAudit_sos_CensusLevelGaps reports the remaining SOS primitives that
// card support census finds missing. Ral Zarek's api:SkipTurn entry was
// removed when the primitive landed.
func TestSetAudit_sos_CensusLevelGaps(t *testing.T) {
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skip("set-audit finding (sos): remaining census gaps")
	}
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	names := []string{
		"Pensive Professor", "Tester of the Tangential", "Textbook Tabulator",
		"Ambitious Augmenter", "Hungry Graffalon", "Topiary Lecturer",
		"Berta, Wise Extrapolator", "Cuboid Colony", "Fractal Tender",
		"Restoration Seminar", "Echocasting Symposium", "Decorum Dissertation",
		"Improvisation Capstone", "Germination Practicum",
		"Choreographed Sparks", "Pox Plague",
	}
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus missing %q -- the sos set cannot be audited", name)
		}
		if m := reg.Unsupported(c, supported); len(m) > 0 {
			t.Errorf("%s still needs %v", name, m)
		}
	}
}
