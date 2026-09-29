package main

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/internal/manabrew/mbtest"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestManabrewLanePlaysCleanAndStampsRecords plays a cardfuzz game through
// the ManaBrew lane (-manabrew) and pins the invariants the flag exists for:
// the game completes cleanly with its replay verified, every decision crossed
// the translator (games counted, prompts posed, zero unmapped, zero
// rejected), the -explore bot policy is inert in the lane, and the failure
// record machinery stamps manabrew=true on a lane record and never on a
// bot-lane one (so -repro rebuilds the right seats from the record alone).
func TestManabrewLanePlaysCleanAndStampsRecords(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	p, err := buildPool(reg)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := loadCov(t.TempDir() + "/none.json")
	r := rand.New(rand.NewPCG(11, 13))
	decks := []genDeck{generate(r, p, c), generate(r, p, c)}

	old := mbCensus
	t.Cleanup(func() { mbCensus = old })

	// The lane: explore=true must be ignored (every seat is a
	// TranslatingSeat; a lane record never carries Explore).
	mbCensus = mbtest.NewCensus()
	fl, gc := playGame(reg, decks, 42, 100, 20000, 20000, true, true, autoPay{mode: "off"})
	if fl != nil {
		t.Fatalf("manabrew lane game failed (%s): %s", fl.Kind, fl.Diag)
	}
	if gc == nil {
		t.Fatal("no game coverage")
	}
	mbCensus.AddGame() // main's fold loop counts each played game the same way
	if mbCensus.Games != 1 {
		t.Fatalf("census recorded %d games, want 1 -- the game did not run through the lane", mbCensus.Games)
	}
	if mbCensus.TotalPosed() == 0 {
		t.Fatal("census recorded zero posed prompts -- no decision crossed the translator")
	}
	if n := mbCensus.TotalUnmapped(); n != 0 {
		t.Fatalf("%d unmapped decision(s): %v", n, mbCensus.Unmapped)
	}
	if n := mbCensus.TotalRejected(); n != 0 {
		t.Fatalf("%d rejected response(s): %v", n, mbCensus.Rejected)
	}
	t.Logf("lane census: %s", mbCensus.Summary())

	// A lane record carries manabrew=true and Explore=false (a setup failure
	// exercises the record path without playing a second full game).
	bad := []genDeck{{Colour: "R", Cards: []string{"Not A Card"}}}
	fl2, _ := playGame(reg, bad, 42, 100, 20000, 20000, true, true, autoPay{mode: "off"})
	if fl2 == nil || fl2.Kind != "setup" {
		t.Fatalf("broken-deck lane game: got %v, want a setup failure", fl2)
	}
	if !fl2.Manabrew {
		t.Fatal("lane record does not carry manabrew=true -- -repro would rebuild bot seats")
	}
	if fl2.Explore {
		t.Fatal("lane record carries Explore=true -- the bot-only knob leaked into the lane")
	}

	// The bot lane's record never carries manabrew.
	mbCensus = nil
	fl3, _ := playGame(reg, bad, 42, 100, 20000, 20000, true, false, autoPay{mode: "off"})
	if fl3 == nil || fl3.Kind != "setup" {
		t.Fatalf("broken-deck bot game: got %v, want a setup failure", fl3)
	}
	if fl3.Manabrew {
		t.Fatal("bot-lane record carries manabrew=true -- -repro would rebuild TranslatingSeats")
	}
}
