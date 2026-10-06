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
var wantCombatCensus = map[string]map[string]int{
	"BIG": {"served:combat.attack": 4, "served:combat.block": 3, "skip:combat block not offered": 1},
	"EOE": {"served:combat.attack": 47, "served:combat.block": 48, "skip:combat block not offered": 1, "skip:combat scenario does not replay": 2},
	"FDN": {"served:combat.attack": 123, "served:combat.block": 122, "skip:combat block not offered": 5, "skip:combat scenario does not replay": 4},
	"FRA": {"served:combat.attack": 62, "served:combat.block": 62, "skip:combat block not offered": 1, "skip:combat scenario does not replay": 1},
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
