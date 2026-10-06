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
// observable, or a counter/speed-gated static that only grants an ability).
// Re-pinned by levelb-static-probe-from-filter, which retries a row the Bear
// shows nothing on with the probes its Affected$ filter names, and by
// levelb-setup-counters-speed, which serves a self-counter-gated static with
// its counters and a MaxSpeed static at speed 4. Measured 2026-10-06 after the
// counter/speed merge onto the probe work. A counter-gated static whose card
// has its own ETB trigger (staticSelfETB) stays on the cast path instead: XMage
// cheats setup permanents onto the battlefield pre-game and never fires the
// trigger, so the counter setup diverges, while the cast path fires it in both
// engines. EOE served 16 -> 23, its needs-counters bucket 30 -> 10, with 13 of
// the counter/speed-gated rows granting only an ability (the named
// staticGrantWaits skip). FDN served 39 -> 40 and its needs-counters bucket
// 3 -> 2. It fails in both directions.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 2,
		"skip:static effect not observable on a probe or the card": 1,
	},
	"EOE": {
		"served": 23,
		"skip:static effect not observable on a probe or the card":                     14,
		"skip:static amount is a computed count the fixture does not make observable":  3,
		"skip:static grants an ability, which waits for levelb-static-granted-ability": 13,
		"skip:static grants only keywords outside the compared evergreen set":          1,
		"skip:static needs counters on the affected permanent":                         10,
	},
	"FDN": {
		"served": 40,
		"skip:static effect not observable on a probe or the card":                    29,
		"skip:static amount is a computed count the fixture does not make observable": 4,
		"skip:static needs counters on the affected permanent":                        2,
	},
	"FRA": {
		"served": 13,
		"skip:static effect not observable on a probe or the card":                    19,
		"skip:static amount is a computed count the fixture does not make observable": 3,
		"skip:static grants only keywords outside the compared evergreen set":         1,
		"skip:static needs a token (setup places none)":                               1,
		"skip:static needs counters on the affected permanent":                        2,
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
