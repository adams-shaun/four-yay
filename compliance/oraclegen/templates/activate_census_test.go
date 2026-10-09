package templates

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// activateCensusSets are the four level-A sets the activate census pins,
// matching compliance/levelb's requirement census.
var activateCensusSets = []string{"BIG", "EOE", "FDN", "FRA"}

// wantActivateCensus pins, per set, every non-gap activate requirement's
// outcome: served, split by sub-family, or skipped, keyed by the skip reason
// with the no-fixture target list folded away. Measured 2026-10-06 with this
// ticket's generator. The test fails in both directions: a template or
// classifier change that changes a count shows up as a diff, and so does a
// stale pin. activate.zone:<Z> requirements (an activation zone other than
// hand or graveyard) are gaps the level-B classifier owns, so they are not
// counted here (compliance/levelb/census_test.go pins them).
//
// The activate.hand and activate.graveyard sub-families are served by this
// template (ticket levelb-fra-activate-zones): the card starts in p0's hand
// or graveyard and the activate step owns its channel-style cost.
//
// Re-measured for the loyalty-headroom/target-fixture ticket
// (cli-20261006T035108Z-725d3fdb) merged with the zone work: a loyalty cost
// above the printed loyalty now gets setup counters, and the legendary /
// +1/+1-counter creature fixtures serve more targets (FDN and FRA
// battlefield counts rose by the +4/+6 main measured); the attackedThisTurn
// combat-prelude shape is still a named skip, and the remaining hand- and
// graveyard-zone requirements merge into the no-fixture bucket.
//
// Re-measured for the Sac<N/filter> fixture-table ticket
// (cli-20261006T071710Z-750ea5c3) merged onto that zone work: the Sac costs
// main pinned as a coarse Sac<...> gap are now cellable from the shared
// fixture table, so EOE and FDN battlefield counts rise (EOE +2, FDN +5) and
// the Sac gap bucket names its own cause (Sac<token>).
//
// Re-measured for the XMage loyalty/duplicate-cost/intrinsic-mana text ticket
// (cli-20261006T035108Z-9e077b0e): the activate requirements formerly skipped
// as "xmage text ambiguous" (planeswalkers, the shared-cost Chandras, the
// Theorist's Sanctum intrinsic mana) are now served; FRA gains one
// SubCounter<...> cost gap from a newly served planeswalker row.
//
// Re-measured for the setup-entry-provenance ticket
// (cli-20261005T142245Z-5f9ed803): setup permanents no longer read as
// entered this turn in either engine, so FRA Gallia, the Merrymaker lost its
// activate.battlefield row and moved to the no-fixture bucket. Its ability is
// "{1}{R}, {T}: Put a +1/+1 counter on target creature that entered this
// turn" (ValidTgts$ Creature.ThisTurnEntered); the row was served only
// BECAUSE the setup battlefield creature wrongly read as entered this turn.
// The ActivateAbility template has no genuine turn-1 entry prelude -- every
// battlefield target fixture is placed at setup -- so it cannot serve the
// filter now. FRA battlefield 71 -> 70 and no-fixture 4 -> 5.
//
// Re-measured for the zone-moving activation-cost ticket
// (cli-20261006T071710Z-a60af507): ExileFromGrave (and the other
// zone-moving Exile/CollectEvidence costs) are now served from the
// activation-cost fixture table, so the EOE ExileFromGrave<...> skip
// becomes a served battlefield requirement (EOE battlefield +1).
//
// Re-measured for the keyword-shaped activation-cost ticket
// (cli-20261006T071710Z-0ab6e98c): a non-loyalty AddCounter<N/KIND> on the
// source is payable with no prerequisite, so the two FDN Mazemind Tome
// AddCounter<1/PAGE> skips become served battlefield requirements (FDN +2).
// Re-measured for cli-20261006T110935Z-0968a39b: seeded setup permanents
// no longer fire ETB triggers. Command Bridge activate#0.0 is now served
// instead of a no-fixture skip (EOE mana 16 -> 17, no-fixture 1 -> 0).
//
// Re-measured for the activation-restriction prelude ticket
// (cli-20261006T132127Z-088fce64): an activated ability whose offer gate
// (IsPresent$, CheckSVar$, Activation$) was false on the bare scenario now
// gets the board/graveyard/turn-history setup the gate names (see
// activate_restriction.go). Merged with main's counter fixtures, which had
// already taken FDN battlefield 81 -> 84; the prelude adds one more
// (FDN battlefield 84 -> 85). FRA battlefield 70 -> 71 and graveyard 7 -> 8
// (no-fixture 5 -> 2). The FRA Proctor of Potential's scry/surveil gate has no
// setup and is now a named restriction gap instead of the generic no-fixture
// bucket.
//
// Re-measured for the activate-ambiguity ticket (cli-20261006T132128Z-8c3cdc9e):
// the six EOE Station lands' "STATION N+" sections no longer count as printed
// ability lines, so their {T} mana ability is served instead of skipped as
// "xmage text ambiguous" (EOE mana 17 -> 23, ambiguous 6 -> 0).
//
// Re-measured for the activation-cost fixture ticket
// (cli-20261006T144108Z-00fe26be): a Sac cost naming a token a maker card can
// produce, and a tapXType total-power/count cost, are served from a prelude
// that makes the tokens; and the tapXType description field no longer glues
// to the filter. EOE Ragost, Deft Gastronaut's {1}{T}, Sacrifice a Food
// ability is served (EOE battlefield 36 -> 37, Sac<token> 1 -> 0); FDN
// Lathril, Blade of the Elves' tapXType<10/Elf> is now recognised as a
// count above the catalogue rather than an unsupported filter (FDN
// unsupported-filter 1 -> 0, count-above-catalogue 0 -> 1).
var wantActivateCensus = map[string]map[string]int{
	"BIG": {
		// +1 served: Worldwalker Helm's "target artifact token you control"
		// gets the Thraben Inspector token prelude (cli-20261009T031408Z-5823e7de).
		"served:activate.battlefield":        12,
		"served:activate.hand":               2,
		"served:activate.mana":               4,
		"skip:activate cost gap: Sac<token>": 1,
	},
	"EOE": {
		// +1 Secluded Starforge activate#0.1: the announced tapXType<X/Artifact>
		// count is served with one tapped catalogue artifact, the X being the
		// tap election's own selection (cli-20261009T031408Z-5823e7de).
		"served:activate.battlefield":             38,
		"served:activate.mana":                    23,
		"skip:activate cost gap: SubCounter<...>": 1,
	},
	"FDN": {
		"served:activate.battlefield":                             89,
		"served:activate.graveyard":                               2,
		"served:activate.mana":                                    53,
		"skip:activate cost gap: tapXType<count-above-catalogue>": 1,
		"skip:activate no fixture":                                1,
	},
	"FRA": {
		// +1 served: Gallia, the Merrymaker's "target creature that entered
		// the battlefield this turn" gets the mid-turn entry candidate
		// (cli-20261009T031408Z-5823e7de).
		"served:activate.battlefield":                                      72,
		"served:activate.graveyard":                                        8,
		"served:activate.hand":                                             9,
		"served:activate.mana":                                             27,
		"skip:activate cost gap: SubCounter<...>":                          1,
		"skip:activation restriction: SVar (Count$YouScryThisTurn/Plus.Y)": 1,
		"skip:activate target gap: attackedThisTurn needs a combat prelude (Creature.attackedThisTurn)": 1,
	},
}

// TestActivateCensus measures every non-gap activate requirement in the four
// level-A sets and pins served versus skipped counts.
func TestActivateCensus(t *testing.T) {
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
				if r.Family != "activate" || r.Gap != "" {
					continue
				}
				_, skip := GenerateB(reg, card, r)
				if skip == nil {
					counts["served:"+r.Sub]++
					continue
				}
				counts[activateSkipBucket(skip.Reason)]++
			}
		}
		got[set] = counts
	}

	for _, set := range activateCensusSets {
		if diff := activateCensusDiff(wantActivateCensus[set], got[set]); diff != "" {
			t.Errorf("%s activate census mismatch:\n%s", set, diff)
		}
	}
}

// activateSkipBucket folds the no-fixture reason's target list away so the
// ratchet groups by cause, not by fixture detail.
func activateSkipBucket(reason string) string {
	if strings.HasPrefix(reason, "activate no fixture") {
		return "skip:activate no fixture"
	}
	return "skip:" + reason
}

// activateCensusDiff reports, in both directions, every bucket whose count
// moved.
func activateCensusDiff(want, got map[string]int) string {
	keys := map[string]bool{}
	for k := range want {
		keys[k] = true
	}
	for k := range got {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var lines []string
	for _, k := range sorted {
		if want[k] != got[k] {
			lines = append(lines, fmt.Sprintf("  %-45s want %4d got %4d", k, want[k], got[k]))
		}
	}
	return strings.Join(lines, "\n")
}
