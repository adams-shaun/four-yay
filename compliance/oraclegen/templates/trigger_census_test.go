package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// wantTriggerCensus pins, per set, every non-gap trigger requirement's
// outcome (served by sub-family, or skipped by reason). trigger.phase is
// L8's and counted as "skip:phase"; a self-ETB trigger level A settles is "covered by level A". It fails in both directions.
var wantTriggerCensus = map[string]map[string]int{
	"BIG": {
		"served:trigger.attacks":    2,
		"served:trigger.dies":       1,
		"served:trigger.spell-cast": 2,
		"skip:phase":                5,
		"skip:trigger.becomes-target: trigger did not fire":  1,
		"skip:trigger.etb-other: trigger covered by level A": 12,
		"skip:trigger.etb-other: trigger did not fire":       1,
	},
	"EOE": {
		"served:trigger.attacks":                                      6,
		"served:trigger.combat-damage":                                1,
		"served:trigger.dies":                                         5,
		"served:trigger.etb-other":                                    1,
		"served:trigger.spell-cast":                                   1,
		"skip:phase":                                                  20,
		"skip:trigger.attacks: trigger did not fire":                  1,
		"skip:trigger.becomes-target: trigger did not fire":           5,
		"skip:trigger.dies: trigger did not fire":                     1,
		"skip:trigger.dies: trigger no recipe: dies needs a creature": 1,
		"skip:trigger.etb-other: trigger covered by level A":          76,
		"skip:trigger.etb-other: trigger did not fire":                17,
		"skip:trigger.spell-cast: trigger did not fire":               5,
	},
	"FDN": {
		"served:trigger.attacks":                             19,
		"served:trigger.becomes-target":                      1,
		"served:trigger.combat-damage":                       7,
		"served:trigger.dies":                                11,
		"served:trigger.drawn":                               3,
		"served:trigger.etb-other":                           10,
		"served:trigger.life-gained":                         8,
		"served:trigger.spell-cast":                          16,
		"skip:phase":                                         26,
		"skip:trigger.attacks: trigger did not fire":         7,
		"skip:trigger.becomes-target: trigger did not fire":  5,
		"skip:trigger.combat-damage: trigger did not fire":   1,
		"skip:trigger.dies: trigger did not fire":            3,
		"skip:trigger.drawn: trigger did not fire":           1,
		"skip:trigger.etb-other: trigger covered by level A": 80,
		"skip:trigger.etb-other: trigger did not fire":       14,
		"skip:trigger.spell-cast: trigger did not fire":      6,
	},
	"FRA": {
		"served:trigger.attacks":                             5,
		"served:trigger.becomes-target":                      1,
		"served:trigger.combat-damage":                       1,
		"served:trigger.dies":                                5,
		"served:trigger.etb-other":                           5,
		"served:trigger.life-gained":                         7,
		"served:trigger.spell-cast":                          17,
		"skip:phase":                                         14,
		"skip:trigger.attacks: trigger did not fire":         1,
		"skip:trigger.becomes-target: trigger did not fire":  5,
		"skip:trigger.combat-damage: trigger did not fire":   1,
		"skip:trigger.etb-other: trigger covered by level A": 65,
		"skip:trigger.etb-other: trigger did not fire":       7,
		"skip:trigger.spell-cast: trigger did not fire":      2,
	},
}

func TestTriggerCensus(t *testing.T) {
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
				if r.Family != "trigger" || r.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, r)
				switch {
				case skip == nil:
					counts["served:"+r.Sub]++
				case r.Sub == "trigger.phase":
					counts["skip:phase"]++
				default:
					counts["skip:"+r.Sub+": "+skip.Reason]++
				}
			}
		}
		got[set] = counts
	}
	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantTriggerCensus[set], got[set]); diff != "" {
			t.Errorf("%s trigger census mismatch:\n%s", set, diff)
		}
	}
}
