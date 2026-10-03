package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestFuzz1003Findings replays the fuzz-1003 cardfuzz records
// (testdata/fuzz1003.jsonl: card-name deck lists, no script text; the recorded
// diag is replaced by a one-line summary) and requires each game to finish
// without a failure:
//
//   - Paradigm (Restoration Seminar, Echocasting Symposium, Improvisation
//     Capstone, Germination Practicum): the exiled card was a free cast at
//     every priority window of its owner's first main phase, so it was cast
//     again after each resolution -- livelocks and intent caps. The copy is
//     now cast once per first main phase (rules paradigmMayPlay).
//   - Mindslaver: on the controlled turn the controller answers the
//     controlled seat's declare-attackers decision, and the attack
//     simulation, which models the deciding seat as the attacker, indexed
//     units[-1] (botpolicy chooseAttackersSim).
//   - Three Required attackers at a two-attacker AttackRestrict ceiling:
//     RequiredQuota counted three while Validate's group cap allowed two, so
//     no declaration was accepted (decision requiredCore).
func TestFuzz1003Findings(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f, err := os.Open("testdata/fuzz1003.jsonl")
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
	if len(recs) != 6 {
		t.Fatalf("testdata holds %d records, want 6", len(recs))
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
