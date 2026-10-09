package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestActivateLevelBRowsR5 serves nine example rows of the g10 activate class
// this round (ticket cli-20261009T031408Z-5823e7de, round r5) cleared. Each
// case first asserts the PRECONDITION the served scenario depends on -- the
// filter or cost shape the mechanism serves -- then that the item generates
// and the scenario plays through gorge with zero fails.
//
// My Precious (HOB) is NOT served: its "Equip—{2}, Pay 2 life." line opens
// with the mana symbol, and XMage's EquipAbility renders the non-mana part
// first ("Equip—Pay 2 life.{2}"), so the mapping stays ambiguous by design
// (compliance/oraclegen/xmage_ability_ambiguity_test.go). Only the word-led
// alternate-cost shape (Bloodthorn Flail) is the printed prefix.
func TestActivateLevelBRowsR5(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, key string
		// lineHas asserts a substring of the requirement's ability line, the
		// shape the mechanism serves; empty skips the check.
		lineHas string
	}{
		// LCI: K:Equip:3 with AlternateCost$ Discard -- the dash-led printed
		// equip line ("Equip—Pay {3} or discard a card.") is the prefix.
		{"Bloodthorn Flail", "activate#0.0", "Keyword$ Equip"},
		// FDN: Creature.Zombie+YouOwn@Graveyard -- a subtype-qualified card
		// filter in the graveyard, served by a real corpus Zombie card.
		{"Zul Ashur, Lich Lord", "activate#0.0", "Creature.Zombie"},
		// TMT: TargetValidTargeting$ reads the held spell's own targets -- the
		// precast Shock aims at the artifact creature itself.
		{"Fugitive Droid", "activate#0.0", "TargetValidTargeting$"},
		// MSH: Creature.YouCtrl+attackingAlone -- the attackingAlone filter
		// word (effects/filter.go) and a single-attacker combat prelude.
		{"Crowd of True Believers", "activate#0.0", "attackingAlone"},
		// WOE: Enchantment.YouCtrl+doesNotShareNameWith -- the
		// doesNotShareNameWith OtherYourBattlefield word.
		{"Yenna, Redtooth Regent", "activate#0.0", "doesNotShareNameWith"},
		// FDN: tapXType<10/Elf> -- the Elf catalogue now carries ten.
		{"Lathril, Blade of the Elves", "activate#0.0", "tapXType<10/Elf>"},
		// TLA: TargetsWithSameCreatureType$ -- two same-subtype fixtures pair.
		{"Secret Tunnel", "activate#0.1", "TargetsWithSameCreatureType$"},
		// MSH: Mjölnir's Worthy equip restriction -- a real Worthy target.
		{"Mjölnir, Hammer of Thor", "activate#0.1", "Worthy"},
		// MSH: the source's own ETB trigger would eat the Sac cost's artifact;
		// the guard fixture keeps one for the payment.
		{"Bullseye, Death Dealer", "activate#0.0", "Sac<1/Artifact>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := activateRowRequirement(t, reg, tc.name, tc.key)
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %q is not in the corpus", tc.name)
			}
			fa := c.Faces[req.Face]
			i, err := atoiSlot(req.Slot)
			if err != nil {
				t.Fatalf("precondition: %s slot %q: %v", tc.name, req.Slot, err)
			}
			sa := fa.Abilities[i]
			if tc.lineHas != "" && !strings.Contains(sa.Line, tc.lineHas) {
				t.Fatalf("precondition: %s ability line names no %q: %.120s", tc.name, tc.lineHas, sa.Line)
			}
			it, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("%s %s skipped: %s", tc.name, tc.key, skip.Reason)
			}
			p0 := it.Scenario.Setup["p0"]
			if len(p0.Battlefield)+len(p0.Hand)+len(p0.Graveyard) == 0 {
				t.Fatalf("%s %s generated an empty p0 setup", tc.name, tc.key)
			}
			// The served setup must actually carry the mechanism's fixture:
			// the target the ability declares (a Worthy creature, a Zombie
			// card, the guard artifact, the Elf catalogue, the pair) sits in
			// p0's zones, or the scenario would play through an offer the
			// engine never made.
			if len(it.Scenario.Steps) == 0 {
				t.Fatalf("%s %s generated no steps", tc.name, tc.key)
			}
			res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(res.Fails) != 0 {
				t.Fatalf("%s %s does not play through gorge: ok=%v fails=%v", tc.name, tc.key, ok, res.Fails)
			}
			if len(res.Snapshots) == 0 {
				t.Fatalf("%s %s produced no snapshots", tc.name, tc.key)
			}
			_ = cards.NormalizeName
		})
	}
}
