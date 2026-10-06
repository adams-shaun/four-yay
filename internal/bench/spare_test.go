package bench_test

// The shared spare-recycling helper (internal/bench/spare.go). Two properties
// the batch runners depend on:
//
//   - a game played on a pooled spare is byte-identical to the same game
//     played on a fresh one, whichever worker drew it (rules.Spare's
//     contract; TestSpareReuseIsInvisible pins the engine side); and
//   - running the pooled player across several workers does not corrupt a
//     game -- the bug a single Spare shared across workers caused on
//     2026-10-05. A SparePool hands each worker its own spare, so the
//     per-game log heads match the single-worker run exactly.
//
// The helper is a pure refactor of the pool cmd/botbench already ran, so
// "same output with and without" is the regression bar. The worker test is
// the one to run under -race (the controller runs it; seats do not).

import (
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// spareTestDecks is a deterministic repo-deck pair for the recycled-game
// tests; the games are short (60-turn cap) so the test fits the per-test
// budget.
const (
	spareTestGames = 6
	spareTestTurns = 60
	spareTestInts  = 4000
)

func sparePair(t *testing.T, reg *cards.Registry) (bench.PairDef, [2][]*cards.Card) {
	t.Helper()
	pd := bench.PairDef{A: "mono-black-aggro", B: "mono-red-prowess"}
	da, err := testutil.LoadRepoDeck(reg, pd.A)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, pd.B)
	if err != nil {
		t.Fatal(err)
	}
	return pd, [2][]*cards.Card{da, db}
}

// playPooledPair runs one pair over RunPairs with a pooled player and returns
// the tallies and each game's log head, in game order. Every game reads its
// head from the finished engine (the last read) before the spare goes back.
func playPooledPair(t *testing.T, reg *cards.Registry, pd bench.PairDef, decks [2][]*cards.Card, workers int) ([]bench.PairResult, []string) {
	t.Helper()
	var pool bench.SparePool
	var mu sync.Mutex
	heads := make([]string, spareTestGames)
	ctor := func(seed uint64) seat.Seat { return seat.NewBot(seed) }
	play := func(pos int, seed uint64, g int, seats [2]seat.Seat) (bench.Outcome, error) {
		cfg := rules.Config{
			Seed: seed, Names: []string{pd.A, pd.B},
			Decks: [][]*cards.Card{decks[0], decks[1]}, Tokens: reg.Tokens,
		}
		sp := pool.Get()
		cfg.Spare = sp
		o, e, err := bench.PlayGame(cfg, seats[:], spareTestTurns, spareTestInts, bench.Hooks{})
		if err != nil || e == nil {
			return o, err
		}
		h := e.L.Head()
		mu.Lock()
		if g >= 0 && g < len(heads) {
			if heads[g] != "" {
				// One worker per game: a second write for the same game would
				// mean the scheduler handed one game to two players.
				t.Errorf("game %d played twice", g)
			}
			heads[g] = h
		}
		mu.Unlock()
		pool.Put(sp, e)
		return o, nil
	}
	res, err := bench.RunPairs(9000, spareTestGames, []bench.PairDef{pd}, ctor, ctor, play, workers, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res, heads
}

// TestSparePooledGameHeadsMatchAcrossWorkers pins byte-identity: the same
// seed set played through pooled spares at 1 and at 4 workers produces the
// same game heads and the same tallies. It exercises the pooled player
// concurrently, so a Spare shared across workers (rather than one per
// worker) shows up here as a divergent head -- and under -race as a data
// race.
func TestSparePooledGameHeadsMatchAcrossWorkers(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pd, decks := sparePair(t, reg)

	oneRes, oneHeads := playPooledPair(t, reg, pd, decks, 1)
	fourRes, fourHeads := playPooledPair(t, reg, pd, decks, 4)

	if !reflect.DeepEqual(oneRes, fourRes) {
		t.Fatalf("pooled RunPairs tally differs at 1 vs 4 workers:\n one=%+v\nfour=%+v", oneRes, fourRes)
	}
	if !reflect.DeepEqual(oneHeads, fourHeads) {
		for g := range oneHeads {
			if oneHeads[g] != fourHeads[g] {
				t.Errorf("game %d: head at 1 worker %q != head at 4 workers %q", g, oneHeads[g], fourHeads[g])
			}
		}
		t.Fatal("pooled games are not byte-identical across worker counts")
	}
	// Precondition: the games were real (a non-empty chain head each). A
	// vacuous run -- no game reached the engine -- must fail loudly here.
	for g, h := range oneHeads {
		if h == "" {
			t.Fatalf("game %d produced no chain head; the pooled run exercised nothing", g)
		}
	}
}

// TestSparePooledMatchesUnpooled pins the other half: recycling a finished
// game's storage through SparePool does not change a game. Each seed is
// played once unpooled (a fresh rules.New) and once on a pooled spare, and
// the heads must match.
func TestSparePooledMatchesUnpooled(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pd, decks := sparePair(t, reg)
	var pool bench.SparePool
	for g := 0; g < spareTestGames; g++ {
		seed := uint64(9100 + g)
		cfg := rules.Config{
			Seed: seed, Names: []string{pd.A, pd.B},
			Decks: [][]*cards.Card{decks[0], decks[1]}, Tokens: reg.Tokens,
		}
		_, fresh, err := bench.PlayGame(cfg, []seat.Seat{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)},
			spareTestTurns, spareTestInts, bench.Hooks{})
		if err != nil {
			t.Fatal(err)
		}
		sp := pool.Get()
		cfg.Spare = sp
		_, recy, err := bench.PlayGame(cfg, []seat.Seat{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)},
			spareTestTurns, spareTestInts, bench.Hooks{})
		if err != nil {
			t.Fatal(err)
		}
		if fresh.L.Head() != recy.L.Head() || len(fresh.L.Events) != len(recy.L.Events) {
			t.Fatalf("seed %d: pooled game diverged from the fresh game (%s/%d vs %s/%d)",
				seed, fresh.L.Head(), len(fresh.L.Events), recy.L.Head(), len(recy.L.Events))
		}
		pool.Put(sp, recy)
	}
}

// TestSparePoolCutsPerGameAllocation proves the pool actually recycles: the
// same seed set played through pooled spares allocates materially less than
// the same games played on a fresh rules.New each time. The log and object
// arrays are a game's largest per-game allocations, so a Put that quietly
// stopped recycling -- or a PlayGameRecycled that forgot to wire cfg.Spare --
// pushes the ratio back to ~1.0 and fails here. The threshold is generous
// (0.8) against a measured ~0.55; the assertion is one-sided so a faster
// machine cannot make it flaky in the wrong direction.
func TestSparePoolCutsPerGameAllocation(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	pd, decks := sparePair(t, reg)
	const games = 8
	measure := func(pooled bool) uint64 {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		var pool bench.SparePool
		for g := 0; g < games; g++ {
			cfg := rules.Config{
				Seed: uint64(9500 + g), Names: []string{pd.A, pd.B},
				Decks: [][]*cards.Card{decks[0], decks[1]}, Tokens: reg.Tokens,
			}
			seats := []seat.Seat{seat.NewBot(cfg.Seed ^ 1), seat.NewBot(cfg.Seed ^ 2)}
			var e *rules.Engine
			var err error
			if pooled {
				sp := pool.Get()
				cfg.Spare = sp
				_, e, err = bench.PlayGame(cfg, seats, spareTestTurns, spareTestInts, bench.Hooks{})
				if err == nil && e != nil {
					pool.Put(sp, e)
				}
			} else {
				_, e, err = bench.PlayGame(cfg, seats, spareTestTurns, spareTestInts, bench.Hooks{})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	fresh := measure(false)
	recycled := measure(true)
	if fresh == 0 {
		t.Fatal("measured zero allocation for the fresh run; the measurement is vacuous")
	}
	t.Logf("total alloc: fresh=%d recycled=%d ratio=%.3f", fresh, recycled, float64(recycled)/float64(fresh))
	if recycled >= fresh*8/10 {
		t.Fatalf("pooled spares allocate %d bytes vs %d fresh (ratio %.3f); the pool is not recycling",
			recycled, fresh, float64(recycled)/float64(fresh))
	}
}
