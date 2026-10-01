package botpolicy

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestIncrementalBoardMatchesScratchOverWholeGames plays whole bot games and
// refills ONE incremental Board for every seat at every decision (the search
// shape: refills alternate seats), and every few decisions a refill on a
// Clone of the engine, so the cache crosses lineages both ways. The test
// binary runs with boardIncVerify on, so every incremental refill is checked
// against a scratch fill of the same state, entry order included; this test
// adds the non-vacuity (the cache reused derivations, and re-derived some)
// and holds DecideBoardFree to Decide on the filled Board at every decision
// it answers, rng untouched.
func TestIncrementalBoardMatchesScratchOverWholeGames(t *testing.T) {
	if testing.Short() {
		t.Skip("plays whole games")
	}
	if !boardIncVerify {
		t.Fatal("the botpolicy test binary must run with boardIncVerify on")
	}
	reg := testutil.CorpusRegistry(t)
	legacy := testutil.LegacyDeckNames()
	type game struct {
		label string
		cfg   rules.Config
	}
	var games []game
	for i := 0; i+1 < len(legacy); i += 2 {
		a, b := legacy[i], legacy[i+1]
		games = append(games, game{"legacy " + a + "," + b, rules.Config{Seed: uint64(70 + i), Names: []string{a, b},
			Decks: [][]*cards.Card{testutil.RepoDeck(t, reg, a), testutil.RepoDeck(t, reg, b)}, Tokens: reg.Tokens}})
	}
	names := legacy[:4]
	decks := make([][]*cards.Card, len(names))
	for k, n := range names {
		decks[k] = testutil.RepoDeck(t, reg, n)
	}
	games = append(games, game{fmt.Sprintf("legacy 4-seat %v", names), rules.Config{Seed: 401, Names: names, Decks: decks, Tokens: reg.Tokens}})

	var rows, derivations, free, freeMain int
	for _, gm := range games {
		e := rules.New(gm.cfg)
		e.Advance()
		board := NewBoard(len(gm.cfg.Names))
		r := rng(gm.cfg.Seed)
		n := 0
		for !e.G.Over && e.Pending() != nil && n < 20000 {
			d := e.Pending()
			if n%7 == 3 {
				// A refill on another lineage: a clone of this very state.
				c := e.Clone()
				BoardFromGameInto(c.G, c, d.Player, &board)
			}
			b := BoardFromGameInto(e.G, e, d.Player, &board)
			rows += b.Creatures.Len() + b.Cards.Len()
			if in, ok := DecideBoardFree(d, e.G.Step.IsMain()); ok {
				free++
				if e.G.Step.IsMain() {
					freeMain++
				}
				r1, r2 := rng(uint64(n)), rng(uint64(n))
				if want := Decide(b, d, r1); !reflect.DeepEqual(in, want) {
					t.Fatalf("%s: decision %d: DecideBoardFree %+v, Decide %+v", gm.label, n, in, want)
				}
				if r1.Uint64() != r2.Uint64() {
					t.Fatalf("%s: decision %d: Decide drew from the rng on a board-free decision", gm.label, n)
				}
			}
			if err := e.Submit(Decide(b, d, r)); err != nil {
				t.Fatalf("%s: decision %d: submit: %v", gm.label, n, err)
			}
			e.Advance()
			n++
		}
		derivations += int(board.inc.derivations)
	}
	if derivations == 0 || derivations*2 > rows || free == 0 || freeMain == 0 {
		t.Fatalf("vacuous: %d derivations for %d rows, %d board-free decisions (%d in a main phase)", derivations, rows, free, freeMain)
	}
	t.Logf("%d derivations for %d rows; %d board-free decisions (%d in a main phase)", derivations, rows, free, freeMain)
}
