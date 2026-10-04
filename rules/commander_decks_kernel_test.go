package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestRepoCommanderDecksPlayAndCastTheirCommanderKernel is the m38 play
// evidence (Ruling M3-P) on the kernel-only engine: one real Commander game
// per interim deck -- command zone, CR 903.8 tax on, 40 life, London
// mulligan -- driven by the test bot to completion and replayed to the same
// chain head; each deck must cast its commander from the command zone at
// least once across its seeds (repoCommanderGames).
func TestRepoCommanderDecksPlayAndCastTheirCommanderKernel(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("long")
	}
	reg := testutil.CorpusRegistry(t)

	for _, g := range repoCommanderGames {
		this, that := g.file, g.opp
		t.Run(this, func(t *testing.T) {
			t.Parallel()
			seeds := []uint64{g.seed}
			if len(g.seeds) > 0 {
				seeds = g.seeds
			}
			deck0 := testutil.RepoDeck(t, reg, this)
			deck1 := testutil.RepoDeck(t, reg, that)
			// The commander's flat position in each resolved deck is the
			// index Config.Commanders wants (genesis moves that object to
			// the command zone); deck.File owns the one shared resolution
			// (deck.CommanderIndex, m39) so gorged cannot disagree with the
			// engine on where a commander sits.
			cmdr0 := testutil.RepoDeckFile(t, this).CommanderIndex()
			cmdr1 := testutil.RepoDeckFile(t, that).CommanderIndex()
			maxCasts := int32(0)
			for _, seed := range seeds {
				cfg := Config{
					Seed:   seed,
					Names:  []string{this, that},
					Decks:  [][]*cards.Card{deck0, deck1},
					Tokens: reg.Tokens,
					Format: FormatCommander,
					// CR 903.9's 40-life start; the m31 CR 903.8 tax and m33
					// 21-damage clock run because FormatCommander is set.
					StartingLife: 40,
					Commanders: [][]int{
						{cmdr0},
						{cmdr1},
					},
					// Mulligans: 1 runs the London keep/mulligan round, as every
					// acceptance game does (R-M1); a commander game that cannot
					// survive its own mulligan round is a deck nobody plays.
					Mulligans: 1,
				}
				e := New(cfg)
				b := newTestBot(g.bot)
				e.Advance()
				n := 0
				for !e.G.Over && e.Pending() != nil && n < 400000 {
					if err := e.Submit(b.answer(e, e.Pending())); err != nil {
						t.Fatalf("%s vs %s, seed %d, intent %d: %v", this, that, seed, n, err)
					}
					n++
				}
				if !e.G.Over {
					t.Fatalf("%s vs %s, seed %d did not finish (turn %d, %d intents)", this, that, seed, e.G.Turn, n)
				}
				if got := e.G.Players[0].CmdCasts[0]; got > maxCasts {
					maxCasts = got
				}
				// Ruling P14: Draw before Winner — Winner's zero value is a real
				// seat (0), so read it only for a non-draw.
				result := "draw"
				if !e.G.Draw {
					result = e.G.Players[e.G.Winner].Name
				}
				t.Logf("%s: seed %d: %6d intents, %6d events, %3d turns, winner=%s, chain=%s",
					this, seed, n, len(e.L.Events), e.G.Turn, result, e.L.Head())

				re, err := replayFor(cfg, e.L)
				if err != nil {
					t.Fatalf("%s: replay: %v", this, err)
				}
				if re.L.Head() != e.L.Head() {
					t.Fatalf("%s: chain %s, replay %s", this, e.L.Head(), re.L.Head())
				}
			}
			if maxCasts < 1 {
				t.Errorf("%s: commander cast from the command zone 0 times across %d seeded game(s), want at least 1", this, len(seeds))
			}
		})
	}
}
