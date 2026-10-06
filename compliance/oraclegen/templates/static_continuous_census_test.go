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
// abilities from the vanilla fixture creature is a named skip), and by
// levelb-static-zone-permissions, which observes a static acting outside the
// battlefield (a play permission or a granted Flashback, Plot or cost
// reduction) as an offered option, a lifelink grant to spells through the
// life it gains, and gives MayLookAt$ its own named skip, and by
// levelb-static-granted-abilities, which observes a granted mana ability or
// loyalty ability and an extra land drop as an offered option and gives every
// other grant shape (activated, triggered and static abilities, abilities
// gained from another card, an SVar) a named skip instead of the generic one,
// and by levelb-static-not-observable-shapes, which serves the rows the
// scenario itself broke (a prelude casting the probe card, a counter-gated base
// without the filter's probes, an Aura whose effect lands on a non-probe host,
// a back-face Equipment never attached): FRA Puppet Crafting moves from skip to
// served.
// It fails in both directions.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 2,
		"skip:static counts cards exiled with the source": 1,
	},
	"EOE": {
		"served": 43,
		"skip:static effect not observable on a probe or the card":                      1,
		"skip:static changes a player rule (hand size, land plays), not a permanent":    1,
		"skip:static needs counters on the affected permanent":                          10,
		"skip:static grants a static ability (observed only through its own effect)":    1,
		"skip:static grants a triggered ability (needs a probe-sourced trigger cause)":  5,
		"skip:static counter-gated card with its own ETB is cast and holds no counters": 1,
		"skip:static gated self grant is not offered in the gate-on fixture":            1,
	},
	"FDN": {
		"served": 65,
		"skip:static effect not observable on a probe or the card":                                   3,
		"skip:static grants an activated ability (needs the driver's activate on a granted ability)": 1,
		"skip:static amount is a computed count the fixture does not make observable":                1,
		"skip:static removes the abilities of a permanent the fixture gives none":                    1,
		"skip:static hand size is not observable in the permanent snapshot":                          1,
		"skip:static needs counters on the affected permanent":                                       2,
	},
	"FRA": {
		"served": 27,
		"skip:static effect not observable on a probe or the card":                                        2,
		"skip:static grants a loyalty ability that adds mana (its offered label names no text to assert)": 1,
		"skip:static grants a loyalty ability the probe planeswalkers cannot pay for":                     2,
		"skip:static grants a static ability (observed only through its own effect)":                      1,
		"skip:static removes the abilities of a permanent the fixture gives none":                         1,
		"skip:static counts cards exiled with the source":                                                 1,
		"skip:static grants only keywords outside the compared evergreen set":                             1,
		"skip:static needs a token (setup places none)":                                                   1,
		"skip:static needs counters on the affected permanent":                                            2,
	},
	// DFT includes the Surveyor cycle's graveyard AddAbility$ grant, whose
	// engine-gap skip remains pinned separately.
	"DFT": {
		"served": 35,
		"skip:static amount is a computed count the fixture does not make observable":            1,
		"skip:static characteristic-defining P/T of a non-creature (the snapshot omits its P/T)": 1,
		"skip:static effect not observable on a probe or the card":                               2,
		"skip:static grants a triggered ability (needs a probe-sourced trigger cause)":           8,
		"skip:static grants only keywords outside the compared evergreen set":                    1,
		"skip:static hand size is not observable in the permanent snapshot":                      1,
		"skip:static removes the abilities of a permanent the fixture gives none":                1,
		"skip:static gated self grant is not offered in the gate-on fixture":                     1,
		"skip:static grants a static ability (observed only through its own effect)":             8,
		"skip:static grants a replacement effect (needs an event the replacement can change)":    2,
		"skip:static granted ability in Graveyard is not offered by the engine":                  4,
	},
}

// staticCensusSets are the sets TestStaticContinuousCensus pins. It extends
// the level-A census sets (activateCensusSets) with DFT, whose Surveyor cycle
// carries the graveyard AddAbility$ grant the engine offers no activation for
// -- pinning DFT is what gives that named reason its own census key.
var staticCensusSets = append([]string{"DFT"}, activateCensusSets...)

func TestStaticContinuousCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	got := map[string]map[string]int{}
	for _, set := range staticCensusSets {
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
	for _, set := range staticCensusSets {
		if diff := activateCensusDiff(wantStaticContinuousCensus[set], got[set]); diff != "" {
			t.Errorf("%s static.continuous census mismatch:\n%s", set, diff)
		}
	}
}
