package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRoundEightReplayFindings replays the round-8 cardfuzz replay-divergence
// records (testdata/round8.jsonl: card-name deck lists, no script text) with
// replay verification on. Each game diverged at a priority event: the live
// engine offered Surge Engine's second ability ("Activate only if CARDNAME
// doesn't have defender", IsPresent$ Card.Self+!withDefender) after its first
// ability removed Defender, and the fresh replay engine did not, because the
// Derived memo stored the empty post-removal keyword list as nil -- which
// SpecContext reads as "unbound, use the printed face" -- in an entry that
// had never held a keyword (rules/derivedmemo.go memoOwned; the rules-side
// pin is TestDerivedMemoKeepsEmptyKeywordListBound).
//
// Seed 20814263898706950 (all-r7) diverged at 011316363 but no longer
// reaches the same board on a base that includes 87586f456 (the fixed-count
// sacrifice plan offer moves the game); it stays pinned as a clean replay.
func TestRoundEightReplayFindings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/round8.jsonl")
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
	oldEnabled := autopayMirror
	t.Cleanup(func() { autopayMirror = oldEnabled })
	autopayMirror = false
	for _, rec := range recs {
		t.Run(fmt.Sprint(rec.Seed), func(t *testing.T) {
			if rec.Kind != "replay" {
				t.Fatalf("record kind %q, want replay", rec.Kind)
			}
			apc, err := autoPayOf(rec)
			if err != nil {
				t.Fatal(err)
			}
			fl, gc := playGame(reg, rec.Decks, rec.Seed, 100, 20000, 20000, true, rec.Explore, apc)
			if fl != nil {
				t.Fatalf("%s: %s", fl.Kind, fl.Diag)
			}
			if gc == nil {
				t.Fatal("no game coverage")
			}
		})
	}
}
