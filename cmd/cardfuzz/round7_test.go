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

// TestRoundSevenFindings replays the round-7 cardfuzz records
// (testdata/round7.jsonl: card-name deck lists, no script text) and pins each
// root cause:
//
//   - mirror 16178228601564090929 (High Stride): the float's Forest tap gave
//     Vintara Snapper shroud before the cast could target it; the stack was
//     already non-empty at the fork, which is what mislabelled it
//     cast_blocked_by_float_trigger. Expected float_removed_every_target.
//   - mirror 17976004960878428008 (Itlimoc, Cradle of the Sun): a non-literal
//     Amount$ is labelled "Add G" beside the plain "Add G"; the float route's
//     label matcher found no option for a GGGG witness
//     (no_matching_mana_option). Now equivalent.
//   - error 3286887339596248657 (Secret Tunnel): SetPropCapacity counted an
//     amassed Sliver Army's repeated "sliver" token twice, offering a
//     same-creature-type activation with no legal pair.
func TestRoundSevenFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/round7.jsonl")
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
	if len(recs) != 3 {
		t.Fatalf("testdata holds %d records, want 3", len(recs))
	}
	oldEnabled, oldOptions := autopayMirror, autopayMirrorOptions
	t.Cleanup(func() { autopayMirror, autopayMirrorOptions = oldEnabled, oldOptions })
	autopayMirrorOptions = paymirror.Options{Control: true}
	want := map[uint64]string{
		16178228601564090929: "unmirrorable expected:float_then_cast:float_removed_every_target",
		17976004960878428008: "",
	}
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
				if v != string(paymirror.Equivalent) && (!strings.Contains(v, "expected:") || v != want[rec.Seed]) {
					t.Errorf("verdict %q x%d", v, c)
				}
			}
			if w := want[rec.Seed]; w != "" && gc.mirrorVerdicts[w] == 0 {
				t.Errorf("verdict %q not reached (verdicts %v)", w, gc.mirrorVerdicts)
			}
			if gc.mirrorVerdicts[string(paymirror.Equivalent)] == 0 {
				t.Errorf("no equivalent planned cast (verdicts %v)", gc.mirrorVerdicts)
			}
		})
	}
}
