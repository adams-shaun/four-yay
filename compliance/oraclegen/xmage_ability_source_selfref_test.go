package oraclegen_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestXMageAbilityRemoveCountersSourceSelfRef pins that a Forge Oracle cost
// that names the source as "this creature" / "this artifact" is rewritten to
// XMage's "{this}" placeholder, because RemoveCountersSourceCost's rule text
// is "remove ... from {this}" (Mage .../costs/common/RemoveCountersSourceCost
// .java). The prefix each want records is what AbilityImpl.getRule() renders;
// the Oracle-spelled prefix XMage rejected is in the comment.
func TestXMageAbilityRemoveCountersSourceSelfRef(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		// Oracle "{1}{G}, Remove two +1/+1 counters from this creature":
		// XMage renders "Remove two +1/+1 counters from {this}".
		{"District Mascot", map[int]string{0: "{1}{G}, Remove two +1/+1 counters from {this}", 1: "Saddle 1"}},
		// Oracle "{1}{R}, Remove a counter from this creature".
		{"Brambleback Brute", map[int]string{0: "{1}{R}, Remove a counter from {this}"}},
		// Oracle "{1}{W}, Remove a counter from this creature".
		{"Burdened Stoneback", map[int]string{0: "{1}{W}, Remove a counter from {this}"}},
		// Oracle "{1}{W/B}, Remove two counters from this creature".
		{"Reaping Willow", map[int]string{0: "{1}{W/B}, Remove two counters from {this}"}},
		// Oracle "{T}, Remove two charge counters from this artifact".
		{"Weather Maker", map[int]string{0: "{T}:", 1: "{T}, Remove two charge counters from {this}", 2: "{T}, Remove three charge counters from {this}"}},
		// Oracle "{T}, Remove three charge counters from this artifact".
		{"Rimefire Torque", map[int]string{0: "{T}, Remove three charge counters from {this}"}},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", tc.card)
		}
		got, why := oraclegen.XMageAbility(c.Faces[0])
		if why != "" {
			t.Fatalf("%s mapping ambiguous: %s", tc.card, why)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("%s mapped %d abilities %q, want %d", tc.card, len(got), got, len(tc.want))
		}
		for i, prefix := range tc.want {
			if got[i] != prefix {
				t.Errorf("%s ability %d prefix = %q, want %q", tc.card, i, got[i], prefix)
			}
		}
	}
}

// TestXMageAbilitySourceSelfRefLeavesCardAlone guards the narrowness of the
// rewrite: a cost that names the source as "this card" (grave/hand exile,
// discard) keeps XMage's "this card" spelling, which the cost classes
// ExileSourceFromGraveCost and DiscardSourceCost render.
func TestXMageAbilitySourceSelfRefLeavesCardAlone(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card string
		want map[int]string
	}{
		{"Spider-Slayer, Hatred Honed", map[int]string{0: "{6}, Exile this card from your graveyard"}},
		{"Sage of the Fang", map[int]string{0: "<i>Renew</i> &mdash; {3}{G}, Exile this card from your graveyard"}},
	} {
		c, ok := reg.Lookup(tc.card)
		if !ok || len(c.Faces) == 0 {
			t.Fatalf("precondition: %s missing from corpus", tc.card)
		}
		got, why := oraclegen.XMageAbility(c.Faces[0])
		if why != "" {
			t.Fatalf("%s mapping ambiguous: %s", tc.card, why)
		}
		for i, prefix := range tc.want {
			if got[i] != prefix {
				t.Errorf("%s ability %d prefix = %q, want %q", tc.card, i, got[i], prefix)
			}
		}
	}
}
