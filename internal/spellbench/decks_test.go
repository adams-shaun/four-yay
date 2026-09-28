package spellbench

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	// rules registers the keyword, trigger, static and replacement
	// primitives into effects.Supported at init; without it every kw:/trig:
	// primitive reads as missing.
	_ "github.com/adams-shaun/gorge/rules"
)

// TestPauperKernelCoverage reports, per catalog deck, the cards gorge does
// not fully support. It is a report, not a ratchet: it fails only on a card
// name the corpus cannot resolve.
func TestPauperKernelCoverage(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sup := effects.Supported()
	ids, err := CatalogIDs(PauperKernel)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		cs, err := Deck(reg, PauperKernel, id)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		seen := map[string]bool{}
		bad := 0
		for _, c := range cs {
			n := c.Faces[0].Name
			if seen[n] {
				continue
			}
			seen[n] = true
			if m := reg.Unsupported(c, sup); len(m) > 0 {
				bad++
				t.Logf("%-9s unsupported: %s %v", id, n, m)
			}
		}
		t.Logf("%-9s %d cards, %d distinct, %d not fully supported", id, len(cs), len(seen), bad)
	}
}
