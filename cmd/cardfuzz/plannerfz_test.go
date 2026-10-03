package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestPlannerFZFindings replays the fuzz-1003 autopay-planner contract
// failures (testdata/plannerfz.jsonl: card-name deck lists, no script text;
// the recorded diag is a one-line summary) with plan failures recorded, and
// requires each game to finish without one:
//
//   - planrev "no legal target for a chained ability" (Stand Together,
//     Rookie Mistake, Swift Kick, Compel Brutality, Incremental Growth): the
//     offer census skipped every TargetUnique$ chain link and never walked a
//     Charm mode's chain, so the planner offered casts the announcement then
//     reversed (rules chainTargetsAvailable / modeTargetsAvailable).
//   - planfb cost_changed (Call to Heel, Press the Enemy, Symbol of
//     Unsummoning beside Battlefield Thaumaturge): the planner's
//     target-dependence gate read only ValidTarget$, so a ReduceCost whose
//     Amount$ counts the targeted creatures was witnessed at the untargeted
//     price (rules costStaticReadsTargets).
func TestPlannerFZFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/plannerfz.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var recs []failure
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var rec failure
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 8 {
		t.Fatalf("testdata holds %d records, want 8", len(recs))
	}
	oldPlan := planFailures
	t.Cleanup(func() { planFailures = oldPlan })
	planFailures = true
	for _, rec := range recs {
		t.Run(fmt.Sprint(rec.Seed), func(t *testing.T) {
			apc, err := autoPayOf(rec)
			if err != nil {
				t.Fatal(err)
			}
			fl, gc := playGame(reg, rec.Decks, rec.Seed, 100, 20000, 20000, true, rec.Explore, apc)
			if fl != nil {
				t.Fatalf("%s (was %s): %s", fl.Kind, rec.Kind, fl.Diag)
			}
			if gc == nil {
				t.Fatal("no game coverage")
			}
		})
	}
}
