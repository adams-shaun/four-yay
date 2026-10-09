package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOtherModeTriggerClassification pins the routing the "other mode" ticket
// (cli-20261009T031408Z-a01003ef) added: LifeLost, a controller-scoped
// Discarded/DiscardedAll and an opponent/player Drawn now name a servable
// sub-family, while an opponent-scoped discard (Tinybones' ValidCard$
// Card.OppOwn) stays a gap because the recipe cannot make an opponent discard.
func TestOtherModeTriggerClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Moonstone Harbinger", "trigger#0.1", "trigger.life-lost"},
		{"Marina Vendrell's Grimoire", "trigger#0.2", "trigger.life-lost"},
		{"Captain Howler, Sea Scourge", "trigger#0.0", "trigger.discarded"},
		{"Moonstone, Harsh Mistress", "trigger#0.0", "trigger.discarded"},
		{"King T'Challa", "trigger#0.0", "trigger.drawn-other"},
		{"Gleaming Splendor", "trigger#0.0", "trigger.drawn-other"},
		{"Tinybones, Bauble Burglar", "trigger#0.0", "trigger.gap:Discarded"},
	} {
		c, ok := reg.Lookup(tc.name)
		if !ok {
			t.Fatalf("%s not in the corpus", tc.name)
		}
		found := false
		for _, r := range levelb.Requirements(c) {
			if r.Key != tc.key {
				continue
			}
			found = true
			if r.Sub != tc.sub {
				t.Fatalf("%s %s classified %s, want %s", tc.name, tc.key, r.Sub, tc.sub)
			}
		}
		if !found {
			t.Fatalf("%s carries no requirement %s", tc.name, tc.key)
		}
	}
}
