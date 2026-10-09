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
// other grant shape (activated and static abilities, abilities gained from
// another card, an SVar) a named skip instead of the generic one, and by
// levelb-granted-triggers, which serves an AddTrigger$ grant by firing the
// granted trigger (its cause steps, not an offered option), and by
// levelb-static-not-observable-shapes, which serves the rows the
// scenario itself broke (a prelude casting the probe card, a counter-gated base
// without the filter's probes, an Aura whose effect lands on a non-probe host,
// a back-face Equipment never attached): FRA Puppet Crafting moves from skip to
// served. This table pins only BIG, EOE, FDN, FRA and DFT; the rows the same
// ticket newly serves in the other sets (WOE, ECL, MSH, LCI, FIN, TLA, OTJ) are
// pinned by TestStaticNotObservableShapes instead.
// Re-pinned by cli-20261009T031407Z-dd0d6fbb, which observes a
// RemoveAllAbilities$ static on a keyworded attach host, a SetMaxHandSize$
// static through the runner's max_hand_size expectation, and a signed
// "for each card in your hand" pump through a hand fixture (DFT, FDN and FRA
// each lose one ability-removal skip; DFT and FDN lose their hand-size skip).
// Re-pinned by cli-20261009T035714Z-53c693da (levelb-granted-static-
// replacement-donor), which observes a granted loyalty ability whose cost
// exceeds the probe's printed loyalty by placing the difference as LOYALTY
// counters (FRA Avatar of Burgeoning Echoes [-10], Kiora of Salt and Sand
// [-8]) and a granted loyalty MANA ability through the ordinary ability offer
// (FRA Way of the Pyromancer "[+1]: Add {R}."), and a GainsAbilitiesOf$ grant
// through a donor card (Marvin, Murderous Mimic; Thranduil, the Elvenking):
// FRA loses all three loyalty-grant skips and its served count rises by three.
// Re-pinned again by agent-20261009T055739Z-b43f3480: a Continuous
// SetMaxHandSize$ static is observed through the runner's max_hand_size
// expectation (the literal rows in FDN and DFT leave their hand-size skip),
// and an AdjustLandPlays$ grant whose source carries a land-entry trigger
// resolves it before the second-land assertion (EOE Icetill Explorer leaves
// the player-rule skip).
// Re-pinned again by agent-20261009T060626Z-a6d0cf40: the Surveyor cycle's
// graveyard AddAbility$ grant is served by the off-battlefield arm of the
// gated-grant observation (the engine offers the granted activation on the
// card in its zone since cli-3b80d13b1), so DFT loses its four
// "granted ability in Graveyard is not offered by the engine" skips and its
// served count rises by four.
// Re-pinned by merge agent-20261009T094023Z-a30f7588 (mrg1): merging the
// AddStaticAbility$ granted-static observation (agent-20261009T094023Z,
// commit 14a747f94) with main's generator serves FRA Tomik, Orzhov Lawmage
// and DFT Racers' Scoreboard, each previously the set's lone
// "grants a static ability" skip; those skip rows leave and each served
// count rises by one.
// Re-pinned again by agent-20261009T055718Z-17e92d7b: a Condition$ NotPlayerTurn
// static (DFT Midnight Mangler) is served by placing the card and advancing the
// scenario to p1's first main phase, so DFT loses its generic observability
// skip.
// It fails in both directions.
var wantStaticContinuousCensus = map[string]map[string]int{
	"BIG": {
		"served": 2,
		"skip:static counts cards exiled with the source": 1,
	},
	"EOE": {
		"served": 62,
		"skip:static gated self grant is not offered in the gate-on fixture": 1,
	},
	"FDN": {
		"served": 68,
		"skip:static effect not observable on a probe or the card":                                   3,
		"skip:static grants an activated ability (needs the driver's activate on a granted ability)": 1,
		"skip:static amount is a computed count the fixture does not make observable":                1,
		"skip:static needs counters on the affected permanent":                                       1,
	},
	"FRA": {
		"served": 35,
		"skip:static effect not observable on a probe or the card":            2,
		"skip:static counts cards exiled with the source":                     1,
		"skip:static grants only keywords outside the compared evergreen set": 1,
	},
	// DFT includes the Surveyor cycle's graveyard AddAbility$ grant, served by
	// the gated-grant observation's off-battlefield arm (agent-
	// 20261009T060626Z-a6d0cf40); its four former engine-gap skips are gone.
	"DFT": {
		"served": 59,
		"skip:static amount is a computed count the fixture does not make observable":            1,
		"skip:static characteristic-defining P/T of a non-creature (the snapshot omits its P/T)": 1,
		"skip:static effect not observable on a probe or the card":                               1,
		"skip:static gated self grant is not offered in the gate-on fixture":                     1,
		"skip:static grants a replacement effect (needs an event the replacement can change)":    2,
	},
}

// staticCensusSets are the sets TestStaticContinuousCensus pins. It extends
// the level-A census sets (activateCensusSets) with DFT, whose Surveyor cycle
// carries the graveyard AddAbility$ grant (served since
// agent-20261009T060626Z-a6d0cf40 and cli-3b80d13b1) -- pinning DFT is what
// keeps the grant's served rows in the census.
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
