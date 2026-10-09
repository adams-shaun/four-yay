package levelb_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestLandPlayedFilterClassification pins the ValidCard gate the land-play
// classifier reads (landPlayedFilterBare): the plain hand land drop serves
// only a filter that accepts any land you play -- empty, a bare `Land`, or
// `Land.YouCtrl` -- while a filter that narrows the land (Shanid's
// `Land.Legendary+YouCtrl`) stays a mode gap, because the recipe's Forest is
// not that land. Preconditions: each card really carries the LandPlayed
// trigger the row cites, with the filter token the case depends on, so
// corpus drift fails loudly instead of passing vacuously.
func TestLandPlayedFilterClassification(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, key, sub, wantFilterToken string }{
		{"The Endstone", "trigger#0.0", "trigger.land-played", "youctrl"},
		{"Recycle", "trigger#0.1", "trigger.land-played", "youctrl"},
		{"Shanid, Sleepers' Scourge", "trigger#0.0", "trigger.gap:LandPlayed", "legendary"},
		{"Gwen Stacy", "trigger#1.0", "trigger.gap:LandPlayed", ""},
	} {
		t.Run(tc.name+"/"+tc.key, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("%s not in the corpus", tc.name)
			}
			if tc.wantFilterToken != "" && !faceHasLandPlayedFilter(c, tc.wantFilterToken) {
				t.Fatalf("precondition: %s carries no LandPlayed trigger filtering on %q", tc.name, tc.wantFilterToken)
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
		})
	}
}

// faceHasLandPlayedFilter reports that some face's triggers include a
// Mode$ LandPlayed trigger whose ValidCard filter carries the token.
func faceHasLandPlayedFilter(c *cards.Card, token string) bool {
	for _, f := range c.Faces {
		for i := range f.Triggers {
			trig := &f.Triggers[i]
			if trig.ModeKind() != cards.TriggerLandPlayed {
				continue
			}
			if strings.Contains(strings.ToLower(trig.ParamStr(cards.PKValidCard)), token) {
				return true
			}
		}
	}
	return false
}
