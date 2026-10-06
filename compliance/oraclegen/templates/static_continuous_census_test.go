package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// wantStaticContinuousCensus pins, per set, how many static.continuous
// requirements the static template serves and how many it skips. Every skip
// is the one observability reason: gorge showed no effect on either probe or
// on the card itself (an Elf/Pirate-only anthem, an ability-only Equipment, a
// self static whose condition the fixture leaves false), or a
// counter/speed-gated static that only grants an ability (staticGrantWaits). It
// fails in both directions.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 2,
		"skip:static effect not observable on a probe or the card": 1,
	},
	"EOE": {
		"served": 36,
		"skip:static effect not observable on a probe or the card":                     19,
		"skip:static grants an ability, which waits for levelb-static-granted-ability": 9,
	},
	"FDN": {
		"served": 27,
		"skip:static effect not observable on a probe or the card": 48,
	},
	"FRA": {
		"served": 10,
		"skip:static effect not observable on a probe or the card": 29,
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
