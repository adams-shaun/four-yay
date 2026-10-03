package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestForbiddenRitualRepeatYesFinding replays the cardfuzz record
// (testdata/forbidden_ritual.jsonl: card-name deck lists, no script text; the
// recorded stack trace is replaced by a one-line summary) that panicked with
// index out of range [-1] in effects.charmGenericPlayersRun. Forbidden
// Ritual's RepeatOptional$ "Repeat this process?" yes resumed through the
// "modes" default arm of resumeAnswerBindingRest, which wrote [""] into
// Ctx.Modes; the next iteration's forced sacrifice did not ask, so the same
// Ctx reached the per-player GenericChoice, which took the stale list as its
// previous chooser's answer and read chooser -1. The rules-level pin is
// rules/resume_modes_test.go; this keeps the original game in the suite.
func TestForbiddenRitualRepeatYesFinding(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	raw, err := os.ReadFile("testdata/forbidden_ritual.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var rec failure
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Seed != 18319407183030670816 {
		t.Fatalf("testdata seed = %d, want the recorded panic's 18319407183030670816", rec.Seed)
	}
	apc, err := autoPayOf(rec)
	if err != nil {
		t.Fatal(err)
	}
	fl, gc := playGame(reg, rec.Decks, rec.Seed, 100, 20000, 20000, true, rec.Explore, apc)
	if fl != nil {
		t.Fatalf("%s: %s", fl.Kind, fl.Diag)
	}
	if gc == nil || !gc.cast["Forbidden Ritual"] {
		t.Fatal("the game no longer casts Forbidden Ritual: it no longer reaches the finding")
	}
}
