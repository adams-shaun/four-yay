package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// TestDoctorsCompanionPrimitiveIsRegistered pins the registration of
// "kw:Doctor's companion" (the WHO two-commander keyword, the
// Doctor's-companion analogue of Companion CR 702.139) in the
// trigger_match.go non-API supported list. The keyword is deck construction,
// not play -- "You can have two commanders if the other is the Doctor." --
// so the registration asserts the corpus shape is understood and the census
// stops counting the carriers unplayable, exactly the class the Partner
// registrations (TestPartnerWithPrimitiveIsRegistered) are. The walk below
// is over the compiled corpus, never a hardcoded count: every K:Doctor's
// companion carrier must no longer name the keyword as a gap, and any
// carrier still unplayable must be blocked on a DIFFERENT, known primitive.
func TestDoctorsCompanionPrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Doctor's companion"] {
		t.Fatal(`effects.Supported() is missing "kw:Doctor's companion"`)
	}
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	var carriers []*cards.Card
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if slices.ContainsFunc(f.Keywords, func(k string) bool {
				return cards.KeywordHead(k) == "Doctor's companion"
			}) {
				carriers = append(carriers, c)
				break
			}
		}
	}
	if len(carriers) == 0 {
		t.Fatal("no K:Doctor's companion carrier found in the corpus; the corpus pin may have moved")
	}
	// The corpus at the pin carries 27 K:Doctor's companion files (the
	// 28th file mentioning the keyword, an_unearthly_child, only references
	// it in DeckNeeds/Valid$); assert the population stays in that
	// neighbourhood so a corpus bump that silently drops or duplicates
	// carriers is loud here.
	if len(carriers) < 25 || len(carriers) > 29 {
		t.Fatalf("K:Doctor's companion carrier population moved: %d (expected ~27)", len(carriers))
	}
	fullySupported := 0
	for _, c := range carriers {
		missing := reg.Unsupported(c, supported)
		if slices.Contains(missing, "kw:Doctor's companion") {
			t.Errorf("%s still names kw:Doctor's companion as an unsupported primitive", c.Path)
			continue
		}
		if len(missing) == 0 {
			fullySupported++
			continue
		}
		t.Logf("%s stays blocked on other gaps: %v", c.Path, missing)
	}
	if fullySupported == 0 {
		t.Fatal("no K:Doctor's companion carrier is fully supported; registration had no census effect")
	}
	t.Logf("%d K:Doctor's companion carriers, %d fully supported", len(carriers), fullySupported)
}

// TestDoctorsCompanionCarrierIsUnderstood pins ONE real corpus carrier
// (Yasmin Khan, a Paradox Power deck card) end to end at the cards tier: the
// K:Doctor's companion line parses onto the face verbatim (head intact,
// apostrophe and all) and Primitives reports the kw: primitive the census
// keyed the gap on. Without the parse half this test would pass with the
// line silently dropped from the face, so the precondition (the keyword is
// on the face) is asserted, not assumed.
func TestDoctorsCompanionCarrierIsUnderstood(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	c, ok := reg.Lookup("Yasmin Khan")
	if !ok {
		t.Fatal("Yasmin Khan not in the corpus; the corpus pin may have moved")
	}
	found := false
	for _, k := range c.Faces[0].Keywords {
		if k == "Doctor's companion" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Yasmin Khan's parsed face lost the keyword line; keywords are %v", c.Faces[0].Keywords)
	}
	prims := c.Primitives()
	if !slices.Contains(prims, "kw:Doctor's companion") {
		t.Errorf("Yasmin Khan's Primitives() = %v does not report kw:Doctor's companion", prims)
	}
	if missing := reg.Unsupported(c, effects.Supported()); slices.Contains(missing, "kw:Doctor's companion") {
		t.Errorf("kw:Doctor's companion is still an unsupported primitive on Yasmin Khan: %v", missing)
	}
}
