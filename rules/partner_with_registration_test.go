package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
)

// TestPartnerWithPrimitiveIsRegistered pins the registration of
// "kw:Partner with" (CR 903.13c, the named-pair alias of kw:Partner) in the
// trigger_match.go non-API supported list. The deck-construction half is not
// play: rules/engine.go's partnerPairOK -> deck.IsPartnerPair seats the mutual
// named pair, so the registration asserts the corpus shape is understood and
// the census stops counting the carriers unplayable. The keyword's OTHER half
// -- CR 702.128's ETB may-search -- is real rules text and is implemented by
// the cards/kw_partner_with.go expansion, pinned end to end by the proof leaf
// in rules/partner_with_etb_test.go. The walk below is over the compiled
// corpus, never a hardcoded count: every K:Partner with carrier must no longer
// name the keyword as a gap, and any carrier still unplayable must be blocked
// on a DIFFERENT, known primitive
// (the measured six: Madame Vastra/Gorm the Great must-block, Pir
// api:ReplaceCounter/repl:AddCounter, Rory api:Investigate, Amy Pond
// api:RemoveCounter/kw:Doctor's companion, Jenny Flint kw:Training).
func TestPartnerWithPrimitiveIsRegistered(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["kw:Partner with"] {
		t.Fatal(`effects.Supported() is missing "kw:Partner with"`)
	}
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	var carriers []*cards.Card
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if slices.ContainsFunc(f.Keywords, func(k string) bool {
				return cards.KeywordHead(k) == "Partner with"
			}) {
				carriers = append(carriers, c)
				break
			}
		}
	}
	if len(carriers) == 0 {
		t.Fatal("no K:Partner with carrier found in the corpus; the corpus pin may have moved")
	}
	fullySupported := 0
	for _, c := range carriers {
		missing := reg.Unsupported(c, supported)
		if slices.Contains(missing, "kw:Partner with") {
			t.Errorf("%s still names kw:Partner with as an unsupported primitive", c.Path)
			continue
		}
		if len(missing) == 0 {
			fullySupported++
			continue
		}
		t.Logf("%s stays blocked on other gaps: %v", c.Path, missing)
	}
	if fullySupported == 0 {
		t.Fatal("no K:Partner with carrier is fully supported; registration had no census effect")
	}
	t.Logf("%d K:Partner with carriers, %d fully supported", len(carriers), fullySupported)
}
