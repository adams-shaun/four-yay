package bench_test

// Guard for PlayGameRecycled's engine-bug-abort rule. Reuse is invisible, so
// the recycle decision has no output effect; this observes it through the
// SparePool itself: seed the pool with a known *rules.Spare, then play an
// engine-bug abort and see whether that same spare comes back. If the abort
// is recycled it does; the fix makes it not.

import (
	"runtime/debug"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// TestSparePoolDropsEngineBugAbort pins that PlayGameRecycled does not recycle
// an engine-bug abort (panic/livelock). A recycled Spare is invisible in a
// game's output, so the observable is identity: the pool is seeded with one
// known spare, an abort is played through PlayGameRecycled, and the same
// pointer must NOT come back.
//
// GC is disabled for the body: sync.Pool clears its per-P and victim caches on
// every GC, so a collection between the seed Put and the assertion would turn
// a real recycling bug into a false pass.
func TestSparePoolDropsEngineBugAbort(t *testing.T) {
	debug.SetGCPercent(-1)
	defer debug.SetGCPercent(100)

	reg := testutil.CorpusRegistry(t)
	pd, decks := sparePair(t, reg)
	seats := func(seed uint64) []seat.Seat {
		return []seat.Seat{seat.NewBot(seed ^ 1), seat.NewBot(seed ^ 2)}
	}
	cfg := func(seed uint64) rules.Config {
		return rules.Config{
			Seed: seed, Names: []string{pd.A, pd.B},
			Decks: [][]*cards.Card{decks[0], decks[1]}, Tokens: reg.Tokens,
		}
	}

	var pool bench.SparePool

	// Seed the pool with a known spare: play a clean game on it and recycle it.
	seedSpare := pool.Get()
	c := cfg(9200)
	c.Spare = seedSpare
	_, e, err := bench.PlayGame(c, seats(c.Seed), spareTestTurns, spareTestInts, bench.Hooks{})
	if err != nil || e == nil {
		t.Fatalf("clean seed game failed: %v", err)
	}
	pool.Put(seedSpare, e) // pool now holds exactly seedSpare

	// Play an engine-bug abort through the helper. Guard fires before the
	// first decision and ends the game as StallOn "livelock" (IsAbort).
	ac := cfg(9201)
	o, err := pool.PlayGameRecycled(ac, seats(ac.Seed), spareTestTurns, spareTestInts,
		bench.Hooks{Guard: func(*rules.Engine) (string, string) { return "livelock", "forced by test" }}, nil)
	if err != nil {
		t.Fatalf("abort game errored: %v", err)
	}
	// Precondition: the game really aborted; if not, the assertion below
	// tests nothing.
	if !bench.IsAbort(o.StallOn) {
		t.Fatalf("precondition: expected an IsAbort outcome, got StallOn=%q", o.StallOn)
	}

	if got := pool.Get(); got == seedSpare {
		t.Errorf("PlayGameRecycled recycled an engine-bug abort: the aborted game's spare came back from the pool")
	}
}
