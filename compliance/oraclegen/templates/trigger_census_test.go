package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// wantTriggerCensus pins, per set, every non-gap trigger requirement's
// outcome (served by sub-family, or skipped by reason). A self-ETB trigger
// level A settles is "covered by level A". It fails in both directions.
var wantTriggerCensus = map[string]map[string]int{
	"BIG": {
		"served:trigger.attacks":                             2,
		"served:trigger.dies":                                1,
		"served:trigger.discarded":                           1,
		"served:trigger.phase":                               5,
		"served:trigger.spell-cast":                          2,
		"served:trigger.becomes-target":                      1,
		"skip:trigger.etb-other: trigger covered by level A": 12,
		"served:trigger.etb-other":                           1,
	},
	"EOE": {
		"served:trigger.attacks":                                      7,
		"served:trigger.attacks-attached":                             2,
		"served:trigger.blocks":                                       1,
		"served:trigger.tapped":                                       8,
		"served:trigger.combat-damage":                                1,
		"served:trigger.dies":                                         6,
		"served:trigger.dies-other":                                   3,
		"served:trigger.etb-land":                                     1,
		"served:trigger.etb-other":                                    18,
		"served:trigger.ltb-other":                                    1,
		"served:trigger.phase":                                        19,
		"served:trigger.spell-cast":                                   6,
		"skip:trigger.attacks: trigger did not fire":                  0,
		"served:trigger.becomes-target":                               4,
		"skip:trigger.becomes-target: trigger did not fire":           1,
		"skip:trigger.dies-other: trigger did not fire":               0,
		"skip:trigger.dies: trigger no recipe: dies needs a creature": 1,
		"skip:trigger.etb-other: trigger covered by level A":          76,
		"skip:trigger.phase: trigger did not fire":                    1,
		"skip:trigger.spell-cast: trigger did not fire":               0,
	},
	"FDN": {
		"served:trigger.attacks":                                26,
		"served:trigger.attacks-attached":                       1,
		"served:trigger.tapped":                                 1,
		"served:trigger.combat-damage":                          8,
		"served:trigger.dies":                                   14,
		"served:trigger.dies-other":                             5,
		"served:trigger.drawn":                                  4,
		"served:trigger.etb-land":                               20,
		"served:trigger.etb-other":                              22,
		"served:trigger.life-gained":                            8,
		"served:trigger.noncombat-damage":                       1,
		"served:trigger.phase":                                  27,
		"served:trigger.spell-cast":                             19,
		"served:trigger.spell-cast-opponent":                    4,
		"skip:trigger.attacks: trigger did not fire":            0,
		"served:trigger.becomes-target":                         6,
		"skip:trigger.dies: trigger did not fire":               0,
		"skip:trigger.etb-other: trigger covered by level A":    80,
		"skip:trigger.etb-other: trigger did not fire":          1,
		"skip:trigger.phase: trigger condition: board presence": 0,
		"skip:trigger.phase: trigger condition: turn history (Count$LifeOppsLostThisTurn)": 0,
		"skip:trigger.phase: trigger did not fire":                                         0,
		"skip:trigger.spell-cast: trigger did not fire":                                    2,
		"skip:trigger.spell-cast: trigger spell-cast opponent-turn condition":              1,
		"skip:trigger.etb-other: trigger no recipe: etb filter OppCtrl (Creature.OppCtrl)": 1,
		"skip:trigger.dies: trigger condition: counters":                                   0,
	},
	// Both of Ruric Thar, Biomagus's prowess instances are served.
	"FRA": {
		"served:trigger.attacks":                             5,
		"served:trigger.attacks-one-target":                  2,
		"served:trigger.combat-damage":                       2,
		"served:trigger.combat-damage-all":                   1,
		"served:trigger.dies":                                5,
		"served:trigger.dies-other":                          5,
		"served:trigger.discarded":                           2,
		"served:trigger.etb-other":                           10,
		"served:trigger.life-gained":                         7,
		"served:trigger.loyalty-activated":                   3,
		"served:trigger.noncombat-damage":                    3,
		"served:trigger.phase":                               11,
		"served:trigger.scry":                                5,
		"served:trigger.spell-cast":                          18, // Ruric Thar, Biomagus: both prowess instances are served.
		"served:trigger.spell-cast-opponent":                 1,
		"served:trigger.surveil":                             5,
		"skip:trigger.attacks: trigger did not fire":         0,
		"served:trigger.becomes-target":                      6,
		"skip:trigger.etb-other: trigger covered by level A": 65,
		"skip:trigger.etb-other: trigger did not fire":       0,
		"skip:trigger.phase: trigger condition: SVar gate (Count$ValidSelf Card.!IsPrepared)":                                   0,
		"skip:trigger.phase: trigger condition: engine predicate unread (!IsPrepared)":                                          3,
		"skip:trigger.phase: trigger condition: turn history (Count$ThisTurnEntered_Graveyard_from_Battlefield_Creature)":       2,
		"skip:trigger.phase: trigger condition: turn history (PlayerCountOpponents$HasPropertywasDealtNonCombatDamageLastTurn)": 1,
		"skip:trigger.phase: trigger did not fire":                                                                              0,
		"skip:trigger.spell-cast-self: trigger covered by level A":                                                              1,
		"skip:trigger.spell-cast: trigger did not fire":                                                                         1,
		"skip:trigger.spell-cast: trigger spell-cast filter predicate gorge does not implement (prepared)":                      1,
		"skip:trigger.etb-other: trigger no recipe: etb filter OppCtrl (Creature.OppCtrl)":                                      1,
		"skip:trigger.phase: trigger did not fire from the graveyard":                                                           0,
		"skip:trigger.attacks: trigger condition: turn history (Count$ThisTurnActivated_Activated)":                             1,
		"skip:trigger.etb-other: trigger condition: board presence":                                                             1,
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
				item, skip := GenerateB(reg, card, r)
				if skip == nil {
					for _, step := range item.Scenario.Steps {
						if step.Op != "pass_to" {
							continue
						}
						checkpoint := step.Step
						if step.Active != "" {
							checkpoint += "@" + step.Active
						}
						if !containsString(PassToSteps(), checkpoint) {
							t.Errorf("%s %s emitted unadvertised pass_to checkpoint %q", set, r.Key, checkpoint)
						}
					}
				}
				switch {
				case skip == nil:
					counts["served:"+r.Sub]++
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
