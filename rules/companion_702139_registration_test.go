package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// Companion is a deck-construction keyword (CR 702.139), as with the Partner
// registrations. This pins recognition of the corpus shape, not the separate
// pregame choice or outside-the-game activation.
func TestCompanionPrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	supported := effects.Supported()
	if !supported["kw:Companion"] {
		t.Fatal(`effects.Supported() is missing "kw:Companion"`)
	}
	reg := searchTestRegistry(t)
	var carriers []*cards.Card
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if slices.ContainsFunc(f.Keywords, func(k string) bool {
				return cards.KeywordHead(k) == "Companion"
			}) {
				carriers = append(carriers, c)
				break
			}
		}
	}
	if len(carriers) < 8 || len(carriers) > 12 {
		t.Fatalf("K:Companion carrier population = %d, expected 8–12", len(carriers))
	}
	fullySupported := 0
	for _, c := range carriers {
		missing := reg.Unsupported(c, supported)
		if slices.Contains(missing, "kw:Companion") {
			t.Errorf("%s still names kw:Companion as unsupported: %v", c.Path, missing)
			continue
		}
		if len(missing) == 0 {
			fullySupported++
		} else {
			t.Logf("%s remains blocked on other gaps: %v", c.Path, missing)
		}
	}
	if fullySupported == 0 {
		t.Fatal("no K:Companion carrier is fully supported; registration had no census effect")
	}
	t.Logf("%d K:Companion carriers, %d fully supported", len(carriers), fullySupported)
}

func TestCompanionCarrierIsUnderstood(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	c, ok := reg.Lookup("Jegantha, the Wellspring")
	if !ok {
		t.Fatal("Jegantha, the Wellspring not in the corpus; the corpus pin may have moved")
	}
	const keyword = "Companion:Special:UniqueManaSymbols:No card in your starting deck has more than one of the same mana symbol in its mana cost."
	if !slices.Contains(c.Faces[0].Keywords, keyword) {
		t.Fatalf("Jegantha's parsed face lacks exact Companion keyword %q; keywords are %v", keyword, c.Faces[0].Keywords)
	}
	primitives := c.Primitives()
	if !slices.Contains(primitives, "kw:Companion") {
		t.Fatalf("Jegantha's Primitives() = %v, want kw:Companion", primitives)
	}
	if missing := reg.Unsupported(c, effects.Supported()); slices.Contains(missing, "kw:Companion") {
		t.Errorf("Jegantha still names kw:Companion as unsupported: %v", missing)
	}
}
