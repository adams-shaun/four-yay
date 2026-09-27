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

// TestRoundNineFindings replays the round-9 cardfuzz records
// (testdata/round9.jsonl: card-name deck lists, no script text; the recorded
// diag is replaced by a one-line summary) and pins each root cause:
//
//   - mirror 17348437687370656270 (Coldsteel Heart): the plan's Leafkin
//     Druid witness was GG ("Add {G}, or {G}{G} with four creatures", a
//     non-literal Amount$ the wheel labels "Add G"); the float route's label
//     matcher took a granted "Add any color" option on the same creature and
//     tapped it for one (production_differs). The plan was right; the float
//     now proves the option on a clone (paymirror verifiedProductions).
//   - planrev 9606575608234985872 (Guiding Bolt, plan-only): Empyrial Armor
//     on an opponent's creature met the Bolt's "power 4 or greater" only
//     while the Bolt was in hand; CR 601.2a moves it to the stack before
//     CR 601.2c, so the planned cast reversed with no legal target. The
//     payment offer now runs the census with the card on the stack
//     (rules/castprobe.go).
func TestRoundNineFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/round9.jsonl")
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
	if len(recs) != 2 {
		t.Fatalf("testdata holds %d records, want 2", len(recs))
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
		})
	}
}
