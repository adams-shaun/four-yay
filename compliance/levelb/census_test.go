package levelb_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// coveredByAKey is the census key holding the number of covered-by-A triggers
// (which carry no sub-family of their own).
const coveredByAKey = "covered-by-A"

// censusSets are the four level-A sets this ticket pins.
var censusSets = []string{"BIG", "EOE", "FDN", "FRA"}

// wantCensus pins the measured requirement census, per set: count per
// Requirement.Sub, plus the number of covered-by-A triggers. Measured
// 2026-10-06 at this commit with a throwaway printer over the same walk
// (same corpus, same CorpusNameFold resolution, same basic-land skip). The
// test fails in both directions: a classifier change that adds or drops a
// requirement shows up as a census diff, and so does a stale pin.
var wantCensus = map[string]map[string]int{
	"BIG": {
		"activate.battlefield":        13,
		"activate.mana":               4,
		"activate.hand":               2,
		"combat.attack":               4,
		"combat.block":                4,
		"covered-by-A":                12,
		"static.continuous":           3,
		"static.disable-triggers":     1,
		"static.gap:CantBeActivated":  1,
		"static.gap:CantBeCast":       1,
		"trigger.attacks":             2,
		"trigger.becomes-target":      1,
		"trigger.dies":                1,
		"trigger.discarded":           1,
		"trigger.etb-other":           1,
		"trigger.gap:ChangesZone":     1,
		"trigger.gap:DamageDealtOnce": 1,
		"trigger.gap:DamageDone":      1,
		"trigger.gap:Sacrificed":      1,
		"trigger.phase":               5,
		"trigger.spell-cast":          2,
	},
	"EOE": {
		"activate.battlefield":           39,
		"activate.mana":                  23,
		"combat.attack":                  49,
		"combat.block":                   49,
		"covered-by-A":                   76,
		"static.combat":                  3,
		"static.combat-damage-toughness": 1,
		"static.continuous":              64,
		"static.cost":                    9,
		"static.gap:CantPreventDamage":   1,
		"static.gap:Panharmonicon":       1,
		"static.gap:TapPowerValue":       1,
		"trigger.attacks":                7,
		"trigger.becomes-target":         5,
		"trigger.combat-damage":          1,
		"trigger.dies":                   7,
		"trigger.dies-other":             3,
		"trigger.etb-land":               1,
		"trigger.etb-other":              18,
		"trigger.gap:AttackerBlocked":    1,
		"trigger.gap:Attacks":            2,
		"trigger.gap:ChangesZone":        5,
		"trigger.gap:CounterAddedOnce":   1,
		"trigger.gap:DamageDone":         2,
		"trigger.gap:DamageDoneOnce":     4,
		"trigger.gap:LandPlayed":         1,
		"trigger.gap:Sacrificed":         2,
		"trigger.gap:Taps":               8,
		"trigger.phase":                  20,
		"trigger.spell-cast":             6,
	},
	"FDN": {
		"activate.battlefield":       91,
		"activate.mana":              53,
		"activate.graveyard":         2,
		"combat.attack":              127,
		"combat.block":               127,
		"covered-by-A":               80,
		"static.cant-block-self":     2,
		"static.combat":              8,
		"static.gap:CantBeActivated": 1,
		"static.continuous":          75,
		"static.cost":                12,
		"static.gap:AlternativeCost": 1,

		"static.gap:CantGainLife":      1,
		"static.gap:CastWithFlash":     1,
		"static.gap:ManaConvert":       1,
		"trigger.attacks":              26,
		"trigger.becomes-target":       6,
		"trigger.combat-damage":        8,
		"trigger.dies":                 14,
		"trigger.dies-other":           5,
		"trigger.drawn":                4,
		"trigger.etb-land":             20,
		"trigger.etb-other":            24,
		"trigger.gap:Always":           1,
		"trigger.gap:Attacks":          1,
		"trigger.gap:ChangesZone":      5,
		"trigger.gap:CounterAddedOnce": 2,
		"trigger.gap:DamageDealtOnce":  1,
		"trigger.gap:DamageDone":       3,
		"trigger.gap:Discarded":        1,
		"trigger.gap:Drawn":            1,
		"trigger.gap:LifeLost":         1,
		"trigger.gap:Sacrificed":       1,
		"trigger.gap:SpellCast":        4,
		"trigger.gap:Taps":             1,
		"trigger.gap:Untaps":           1,
		"trigger.life-gained":          8,
		"trigger.noncombat-damage":     1,
		"trigger.phase":                27,
		"trigger.spell-cast":           22,
	},
	"FRA": {
		"activate.battlefield":                         74,
		"activate.mana":                                27,
		"activate.graveyard":                           9,
		"activate.hand":                                9,
		"combat.attack":                                63,
		"combat.block":                                 63,
		"covered-by-A":                                 66,
		"static.can-attack-defender":                   1,
		"static.can-attack-defender-svar":              1,
		"static.cant-be-activated-combat":              1,
		"static.cant-be-cast-combat":                   1,
		"static.cant-be-cast-threshold":                1,
		"static.cant-block-by":                         1,
		"static.combat":                                1,
		"static.combat-damage-toughness":               1,
		"static.continuous":                            39,
		"static.cost":                                  7,
		"static.disable-triggers":                      1,
		"static.gap:IgnorePlaneswalkerZeroLoyaltyRule": 1,
		"trigger.attacks":                              6,
		"trigger.attacks-one-target":                   2,
		"trigger.becomes-target":                       6,
		"trigger.combat-damage":                        2,
		"trigger.combat-damage-all":                    1,
		"trigger.dies":                                 5,
		"trigger.dies-other":                           5,
		"trigger.discarded":                            2,
		"trigger.etb-other":                            12,
		"trigger.gap:AbilityCast":                      2,
		"trigger.gap:Blocks":                           1,
		"trigger.gap:SpellCast":                        1,
		"trigger.life-gained":                          7,
		"trigger.loyalty-activated":                    3,
		"trigger.noncombat-damage":                     3,
		"trigger.phase":                                17,
		"trigger.scry":                                 5,
		"trigger.spell-cast":                           19,
		"trigger.surveil":                              5,
	},
}

// TestLevelBRequirementCensus pins the level-B requirement census for the
// four level-A sets. Names are resolved exactly as gate.Check resolves them
// (compliance.CorpusNameFold through compliance.FoldedNames), and basic lands
// are skipped exactly as gate.Check skips them.
func TestLevelBRequirementCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	root := filepath.Join("..", "..")
	has := func(n string) bool { _, ok := reg.Lookup(n); return ok }
	folded := compliance.FoldedNames(reg)

	got := map[string]map[string]int{}
	for _, set := range censusSets {
		pr, err := compliance.LoadPrinted(filepath.Join(root, "compliance", "printed"), set)
		if err != nil {
			t.Fatalf("%s: %v", set, err)
		}
		counts := map[string]int{}
		for _, printed := range pr.Cards {
			name, ok := compliance.CorpusNameFold(has, folded, printed)
			if !ok {
				t.Errorf("%s: %q not in the corpus", set, printed)
				continue
			}
			c, _ := reg.Lookup(name)
			if isBasicLand(c) {
				continue
			}
			for _, r := range levelb.Requirements(c) {
				if r.CoveredByA {
					counts[coveredByAKey]++
					continue
				}
				counts[r.Sub]++
			}
		}
		got[set] = counts
	}

	for _, set := range censusSets {
		if !equalCounts(got[set], wantCensus[set]) {
			t.Errorf("%s census mismatch:\n%s", set, diffCounts(wantCensus[set], got[set]))
		}
	}
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// diffCounts reports, in both directions, every key whose count moved.
func diffCounts(want, got map[string]int) string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	var lines []string
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		if want[k] != got[k] {
			lines = append(lines, fmt.Sprintf("  %-40s want %4d got %4d", k, want[k], got[k]))
		}
	}
	if len(lines) == 0 {
		return "  (no key differs; total map size differs)"
	}
	return strings.Join(lines, "\n")
}

// isBasicLand mirrors gate.isBasicLand (unexported there).
func isBasicLand(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 {
		return false
	}
	basic, land := false, false
	for _, ty := range c.Faces[0].Types {
		basic = basic || strings.EqualFold(ty, "Basic")
		land = land || strings.EqualFold(ty, "Land")
	}
	return basic && land
}
