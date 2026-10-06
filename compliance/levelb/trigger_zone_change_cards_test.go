package levelb_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestZoneChangeRealCardSubFamilies classifies the corpus cards the zone-change
// brief names: each key carries exactly the sub-family below, and a card or key
// absent from the corpus fails loudly rather than skipping the row.
func TestZoneChangeRealCardSubFamilies(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub string }{
		{"Ark of Hunger", "trigger#0.0", "trigger.leaves-graveyard"},
		{"Spirit Mascot", "trigger#0.0", "trigger.leaves-graveyard"},
		{"Ninja Teen", "trigger#0.0", "trigger.ltb-other"},
		{"Ketramose, the New Dawn", "trigger#0.0", "trigger.ltb-other"},
		{"Kaya, Spirits' Justice", "trigger#0.0", "trigger.ltb-other"},
		{"Elvish Archivist", "trigger#0.0", "trigger.etb-other"},
		{"Caretaker's Talent", "trigger#0.0", "trigger.etb-other"},
		{"Mister Fantastic, Reed Richards", "trigger#0.0", "trigger.etb-other"},
		{"Zenos yae Galvus", "trigger#0.1", "trigger.zone-change-residue"},
		{"Hedge Shredder", "trigger#0.1", "trigger.zone-change-residue"},
	} {
		t.Run(tc.name+" "+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s is not in the corpus", tc.name)
			}
			for _, r := range levelb.Requirements(c) {
				if r.Key != tc.key {
					continue
				}
				if r.Sub != tc.sub {
					t.Fatalf("%s %s: sub = %q, want %q", tc.name, tc.key, r.Sub, tc.sub)
				}
				return
			}
			t.Fatalf("precondition: %s carries no requirement %s", tc.name, tc.key)
		})
	}
}
