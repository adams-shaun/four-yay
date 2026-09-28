// deadly_disguise_deck_test.go — the Deadly Disguise (MKC) Commander precon
// import. The deck is a face-down/morph-family list; both family shapes are
// now implemented: the printed-LAND face-down cast (Zoetic Cavern, Branch of
// Vitu-Ghazi; rules/morph_land_test.go) and the full non-mana/announced-X
// turn-face-up cost (rules/morph_turnup.go, rules/morph_turnup_cost_test.go).
// So kw:Morph / kw:Megamorph / kw:Disguise are IN effects.Supported() and the
// deck's 24 carriers are fully supported, none named in knownUnsupported.
// These tests pin that honest state: the deck is seated by the ratchet and
// every carrier measures clean.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// morphHeads are the three family heads the deck's carriers print.
var morphHeads = []string{"kw:Morph", "kw:Megamorph", "kw:Disguise"}

// morphHeadOn returns the family head a face prints, or "".
func morphHeadOn(f interface {
	KeywordParam(string) (string, bool)
}) string {
	for _, h := range []string{"Morph", "Megamorph", "Disguise"} {
		if _, ok := f.KeywordParam(h); ok {
			return "kw:" + h
		}
	}
	return ""
}

// TestDeadlyDisguiseDeckIsSeatedByTheRatchet asserts the imported precon is
// a repo deck the acceptance and param-census ratchets seat: RepoDeckNames
// must name it, its embedded file must declare the commander, and resolving
// it against the corpus must yield 100 cards. A missing file, a bad commander
// or an unresolvable card name fails here before either ratchet's own
// message can be misread as a card-behaviour gap.
func TestDeadlyDisguiseDeckIsSeatedByTheRatchet(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	found := false
	for _, name := range testutil.RepoDeckNames() {
		if name == "deadly-disguise" {
			found = true
		}
	}
	if !found {
		t.Fatalf("RepoDeckNames() does not include %q: %v", "deadly-disguise", testutil.RepoDeckNames())
	}

	f := testutil.RepoDeckFile(t, "deadly-disguise")
	if f.Commander != "Kaust, Eyes of the Glade" {
		t.Fatalf("deadly-disguise commander = %q, want Kaust, Eyes of the Glade", f.Commander)
	}
	if f.Format != "commander" {
		t.Fatalf("deadly-disguise format = %q, want commander", f.Format)
	}

	// PRECONDITION: the deck is a real 100-card Commander list, not a stub.
	// Count the resolved slice (count-many-times repeated) rather than the
	// JSON entries, so a wrong count is caught too.
	cs := testutil.RepoDeck(t, reg, "deadly-disguise")
	if len(cs) != 100 {
		t.Fatalf("deadly-disguise resolved to %d cards, want 100", len(cs))
	}
	if err := f.ValidateCommander(reg); err != nil {
		t.Fatalf("deadly-disguise is not a legal Commander deck: %v", err)
	}
}

// TestDeadlyDisguiseMorphCarriersAreFullySupported pins the honest
// measurement the turn-face-up cost work makes true: all three heads ARE in
// effects.Supported() now that the printed-LAND face-down cast and the full
// non-mana/announced-X turn-up cost are implemented, and every morph-family
// carrier in the deck (the 24 that were the measured gap) is fully supported
// -- none is named in knownUnsupported.
//
// The count is asserted non-zero first so the per-card loop cannot pass
// vacuously over an empty set.
func TestDeadlyDisguiseMorphCarriersAreFullySupported(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()

	for _, head := range morphHeads {
		if !supported[head] {
			t.Fatalf("effects.Supported() does not claim %q; the cast and turn-up shapes are implemented", head)
		}
	}

	// PRECONDITION: the deck genuinely carries morph-family cards. Without
	// this, the per-card loop below would pass vacuously over an empty set.
	carriers := 0
	for _, c := range testutil.RepoDeck(t, reg, "deadly-disguise") {
		head := morphHeadOn(c.Faces[0])
		if head == "" {
			continue
		}
		carriers++
		if got := reg.Unsupported(c, supported); len(got) > 0 {
			t.Errorf("%s carries %s but still measures unsupported primitives %v", c.Faces[0].Name, head, got)
		}
		if want, ok := knownUnsupported[c.Faces[0].Name]; ok {
			t.Errorf("%s is fully supported now -- delete it from knownUnsupported (was %v)", c.Faces[0].Name, want)
		}
	}
	if carriers == 0 {
		t.Fatal("no Deadly Disguise card carries Morph/Megamorph/Disguise — the deck or the keyword read is wrong")
	}
}
