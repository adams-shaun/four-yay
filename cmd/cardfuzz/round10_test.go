package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/paymirror"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoundTenFindings replays the round-10 cardfuzz records
// (testdata/round10.jsonl: card-name deck lists, no script text; the recorded
// diag is replaced by a one-line summary) and pins each root cause:
//
//   - mirror 6858100184279525408 (Tunnel Vision) and 9099806368093434011
//     (Springleaf Drum): float_then_cast state_differs on
//     G.Objs[*].Remembered[*].Obj with trigger_push on both sides. A CR 603.8
//     state trigger (Mode$ Always) remembered the object of whichever event
//     ran its condition check -- a land tapped inside run A's CR 601.2g
//     window, nothing on the float route. It now remembers its source
//     (rules stateTriggerRemembered).
//   - livelock 12282246443002902452 (Jadelight Spelunker, "explores X times",
//     X = 6): every nonland explore suspends on its put-back-or-graveyard
//     election and the resumed effExplore restarted its Num$ count, so it
//     explored without end. The count now rides the election
//     (Decision.ResumeExploreDone -> Ctx.ExploreCount).
//   - mirror-r9 12931427917867112279 (Panther Robot, affinity for artifacts):
//     float_then_cast cast_not_offered_after_float. The float sacrificed the
//     plan's artifact mana source at priority, raising the cast's price from
//     {8} to {9}; run A's total cost was locked in (CR 601.2f) before its
//     CR 601.2g window sacrificed the same source. Expected
//     float_raised_cost, proven by paymirror floatRaisedCost. The recorded
//     seed 12931427917867112207 no longer reached it once Imperial
//     Cosmographer's "leaves the battlefield without dying" trigger (Destination$
//     Ante,Command,Exile,Hand,Library) started firing on the opponent's Unsummon
//     bounce (zone-change Destination$ lists are now a zone set), which changes
//     the game; the same decks at seed 12931427917867112279 reach the verdict.
func TestRoundTenFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/round10.jsonl")
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
	if len(recs) != 4 {
		t.Fatalf("testdata holds %d records, want 4", len(recs))
	}
	// seed -> a verdict the game must reach (beyond "no failure").
	wantVerdict := map[uint64]string{
		12931427917867112279: "expected:float_then_cast:float_raised_cost",
	}
	oldEnabled, oldOptions, oldPlan := autopayMirror, autopayMirrorOptions, planFailures
	t.Cleanup(func() { autopayMirror, autopayMirrorOptions, planFailures = oldEnabled, oldOptions, oldPlan })
	autopayMirrorOptions = paymirror.Options{Control: true}
	planFailures = true
	for _, rec := range recs {
		t.Run(fmt.Sprint(rec.Seed), func(t *testing.T) {
			apc, err := autoPayOf(rec)
			if err != nil {
				t.Fatal(err)
			}
			autopayMirror = rec.Kind == "mirror"
			fl, gc := playGame(reg, rec.Decks, rec.Seed, 100, 20000, 20000, true, rec.Explore, apc)
			if fl != nil {
				t.Fatalf("%s: %s", fl.Kind, fl.Diag)
			}
			if gc == nil {
				t.Fatal("no game coverage")
			}
			for _, mf := range gc.mirrorFailures {
				t.Errorf("mirror failure %s: %s", mf.Sig, mf.Diag)
			}
			if !autopayMirror {
				return
			}
			for v, c := range gc.mirrorVerdicts {
				if v != string(paymirror.Equivalent) && !strings.Contains(v, "expected:") {
					t.Errorf("verdict %q x%d", v, c)
				}
			}
			if gc.mirrorVerdicts[string(paymirror.Equivalent)] == 0 {
				t.Errorf("no equivalent planned cast (verdicts %v)", gc.mirrorVerdicts)
			}
			if w := wantVerdict[rec.Seed]; w != "" {
				found := false
				for v := range gc.mirrorVerdicts {
					if strings.Contains(v, w) {
						found = true
					}
				}
				if !found {
					t.Errorf("no %q verdict (verdicts %v): the game no longer reaches the finding", w, gc.mirrorVerdicts)
				}
			}
		})
	}
}
