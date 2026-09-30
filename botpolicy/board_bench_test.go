package botpolicy_test

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// boardBenchSnapshots plays one real-deck game (mono-red-prowess vs
// mono-blue-tempo, the searchprobe bench pair) bot vs bot and keeps an
// engine clone every stride intents: realistic boards of every game phase
// for the per-decision Board refill benchmarks below.
func boardBenchSnapshots(tb testing.TB) []*rules.Engine {
	tb.Helper()
	reg := testutil.CorpusRegistry(tb)
	names := []string{"mono-red-prowess", "mono-blue-tempo"}
	decks := make([][]*cards.Card, len(names))
	for i, n := range names {
		var err error
		if decks[i], err = testutil.LoadRepoDeck(reg, n); err != nil {
			tb.Fatal(err)
		}
	}
	e := rules.New(rules.Config{Seed: 30_000_000, Names: names, Decks: decks, Tokens: reg.Tokens})
	e.Advance()
	rngs := []*rand.Rand{rand.New(rand.NewPCG(7, 0)), rand.New(rand.NewPCG(7, 1))}
	board := botpolicy.NewBoard(len(names))
	const stride = 25
	var snaps []*rules.Engine
	for i := 0; i < 5000 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if i%stride == 0 {
			snaps = append(snaps, e.Clone())
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if err := e.Submit(in); err != nil {
			tb.Fatal(err)
		}
	}
	if len(snaps) < 8 {
		tb.Fatalf("bench game produced only %d snapshots", len(snaps))
	}
	return snaps
}

// BenchmarkBoardFromGameInto is the per-decision Board refill the host match
// loop and every search rollout pay: one BoardFromGameInto into a reused
// Board, round-robin over a whole game's snapshots.
func BenchmarkBoardFromGameInto(b *testing.B) {
	snaps := boardBenchSnapshots(b)
	board := botpolicy.NewBoard(2)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := snaps[i%len(snaps)]
		botpolicy.BoardFromGameInto(e.G, e, e.Pending().Player, &board)
	}
}

// BenchmarkBoardFromGameIntoDecide is the refill plus the Decide it feeds:
// the whole per-decision bot cost.
func BenchmarkBoardFromGameIntoDecide(b *testing.B) {
	snaps := boardBenchSnapshots(b)
	board := botpolicy.NewBoard(2)
	rng := rand.New(rand.NewPCG(9, 9))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := snaps[i%len(snaps)]
		d := e.Pending()
		botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rng)
	}
}
