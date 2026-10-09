package templates

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// wantCombatCensus pins, per set, every non-gap combat requirement's outcome.
//
// Re-measured for the setup-entry-provenance ticket
// (cli-20261005T142245Z-5f9ed803): setup permanents no longer read as
// entered this turn in either engine, so EOE Mechan Shieldmate lost its
// combat.attack row and moved to the scenario-does-not-replay bucket. The
// creature has defender and "As long as an artifact entered the battlefield
// under your control this turn, this creature can attack as though it didn't
// have defender" (CanAttackDefender gated on
// Count$ThisTurnEntered_Battlefield_Artifact.YouCtrl); the attack row was
// served only BECAUSE a setup battlefield artifact wrongly read as entered
// this turn. With the artifact correctly old, X = 0 and the defender cannot
// attack, so the scenario no longer replays. EOE attack 48 -> 47 and
// scenario-does-not-replay 1 -> 2 (the other is Monoist Sentry, whose plain
// Defender genuinely cannot attack).
//
// Re-measured for the combat-legality ticket (cli-20261006T132127Z-b96121b3):
// a Defender creature whose attack gorge refuses is now served by an
// attacker-not-offered observation (combat_legality.go) instead of skipping,
// and a creature with an unconditional "CARDNAME can't block" is served by a
// blocker-not-offered one. EOE both Defender rows (Mechan Shieldmate,
// Monoist Sentry) moved from scenario-does-not-replay to served; FDN four
// Defender attack rows did, and two CantBlock rows moved from block-not-offered
// to served; FRA's one Defender row did. A block requirement for a creature
// that does not survive to the opponent's turn from setup is served by the
// late-entry block (combat_late_entry.go): FDN Ball Lightning, FRA Frostbite
// Pyromental.
// Re-measured for the G6 combat-replay ticket
// (cli-20261009T031408Z-eeb9a525): a self CantAttack/CantBlock static whose
// gorge refuses the action is served by an attacker/blocker-not-offered
// observation (combat_self_restrict.go); a fixture that loses the card itself
// is served by the fixture-shape serves (combat_setup_fix.go: died-at-setup
// counters, the second-card-draw untap, the stun untap, the sac-or-tap late
// entry); and a self MustAttack (Flamewake Phoenix, Juggernaut) or a priced
// CantAttackUnless (Archangel of Tithes, other sets) is served by the
// late-entry block, whose structural-cause guard
// (combat_late_entry.go) accepted the script-anchored causes. FDN's two
// block-not-offered rows (Flamewake Phoenix, Juggernaut) moved to served.
var wantCombatCensus = map[string]map[string]int{
	"BIG": {"served:combat.attack": 4, "served:combat.block": 4},
	"EOE": {"served:combat.attack": 49, "served:combat.block": 49},
	"FDN": {"served:combat.attack": 127, "served:combat.block": 127},
	"FRA": {"served:combat.attack": 63, "served:combat.block": 63},
}

func TestCombatCensusB(t *testing.T) {
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
				t.Errorf("%s: %q not in corpus", set, name)
				continue
			}
			c, _ := reg.Lookup(card)
			for _, req := range levelb.Requirements(c) {
				if !combatSubs(req.Sub) || req.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, req)
				if skip == nil {
					counts["served:"+req.Sub]++
				} else {
					counts["skip:"+skip.Reason]++
				}
			}
		}
		got[set] = counts
	}
	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantCombatCensus[set], got[set]); diff != "" {
			t.Errorf("%s combat census mismatch:\n%s", set, diff)
		}
	}
}
