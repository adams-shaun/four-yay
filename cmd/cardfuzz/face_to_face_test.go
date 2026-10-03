package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestFaceToFaceMatchEnds replays the fuzz-1003 Face to Face livelock
// (testdata/face_to_face.jsonl: card-name deck lists, no script text; the
// recorded diag is a one-line summary). Both bots answered the throw's
// GenericChoice with option 0, so Rock tied Rock forever; the throw carries
// AILogic$ Random, which now rides the ask as decision.Decision.AIRandom and
// is drawn from each seat's seeded rng, so the match ends.
func TestFaceToFaceMatchEnds(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("testdata/face_to_face.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var rec failure
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
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
}
