package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticLibraryEmpty: Living Conundrum's "as long as there are no cards in
// your library" 10/10 flying vigilance (PresentZone$ Library, PresentCompare$
// EQ0) was unobservable because both engines pad each library to 40 cards. The
// fixture names 40 cards in the graveyard, which leaves nothing to pad the
// library with. The precondition asserts the printed card is 2/5 with neither
// granted keyword and that the final library really is empty, so the test
// cannot pass on a card the bare scenario already pumped.
func TestStaticLibraryEmpty(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Living Conundrum"
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if got := c.Faces[0].PT; got != "2/5" {
		t.Fatalf("precondition: %s printed P/T = %q, want 2/5", name, got)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	if got := len(it.Setup["p0"].Graveyard); got < 40 {
		t.Fatalf("%s: p0 graveyard names %d cards, want >= 40 so the library is not padded", name, got)
	}
	if got := final.Players[0].LibraryCount; got != 0 {
		t.Fatalf("%s: p0 library holds %d cards at the final checkpoint, want 0", name, got)
	}
	p, ok := permNamedSnap(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if p.PT != "10/10" {
		t.Fatalf("%s P/T = %s, want 10/10 with an empty library", name, p.PT)
	}
	for _, kw := range []string{"Flying", "Vigilance"} {
		if !slices.Contains(p.Keywords, kw) {
			t.Fatalf("%s keywords = %v, want %s granted", name, p.Keywords, kw)
		}
	}
}
