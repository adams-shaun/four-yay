package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// wantStaticContinuousCensus pins, per set, how many static.continuous
// requirements the static template serves and how many it skips, keyed by the
// skip reason. A skip is either the generic observability reason (gorge showed
// no effect on any probe or the card: a conditional self static the fixture
// leaves false, or a filter gorge does not apply) or a narrower named one (a
// qualifier the setup cannot give a probe, a grant outside the compared
// evergreen keywords, an amount counted from state the fixture does not make
// observable). Re-pinned by levelb-static-probe-from-filter, which retries a
// row the Bear shows nothing on with the probes its Affected$ filter names,
// and by levelb-static-cda-types-control, which also observes a card's own
// characteristic-defining P/T, a type or colour change and a control change
// (Eluge-style CDAs, Tractor Beam, Ygra move out of the skips; a removal of
// abilities from the vanilla fixture creature is a named skip).
// It fails in both directions.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 2,
		"skip:static counts cards exiled with the source": 1,
	},
	"EOE": {
		"served": 24,
		"skip:static effect not observable on a probe or the card":                   8,
		"skip:static changes a player rule (hand size, land plays), not a permanent": 1,
		"skip:static grants only keywords outside the compared evergreen set":        1,
		"skip:static needs counters on the affected permanent":                       30,
	},
	"FDN": {
		"served": 54,
		"skip:static effect not observable on a probe or the card":                    14,
		"skip:static amount is a computed count the fixture does not make observable": 1,
		"skip:static removes the abilities of a permanent the fixture gives none":     1,
		"skip:static changes a player rule (hand size, land plays), not a permanent":  2,
		"skip:static needs counters on the affected permanent":                        3,
	},
	"FRA": {
		"served": 19,
		"skip:static effect not observable on a probe or the card":                14,
		"skip:static removes the abilities of a permanent the fixture gives none": 1,
		"skip:static counts cards exiled with the source":                         1,
		"skip:static grants only keywords outside the compared evergreen set":     1,
		"skip:static needs a token (setup places none)":                           1,
		"skip:static needs counters on the affected permanent":                    2,
	},
}

func TestStaticContinuousCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	got := map[string]map[string]int{}
	for _, set := range activateCensusSets {
		printed, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		counts := map[string]int{}
		for _, name := range printed.Cards {
			card, ok := compliance.CorpusNameFold(has, folded, name)
			if !ok {
				t.Errorf("%s: %q not in the corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, r := range levelb.Requirements(c) {
				if r.Sub != "static.continuous" || r.Gap != "" {
					continue
				}
				if _, skip := GenerateB(reg, card, r); skip != nil {
					counts["skip:"+skip.Reason]++
				} else {
					counts["served"]++
				}
			}
		}
		got[set] = counts
	}
	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantStaticContinuousCensus[set], got[set]); diff != "" {
			t.Errorf("%s static.continuous census mismatch:\n%s", set, diff)
		}
	}
}
