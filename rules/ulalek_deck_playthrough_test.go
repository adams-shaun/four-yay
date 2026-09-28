package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// The ulalek-eldrazi commander deck plays complete bot games against the
// valgavoth commander deck at the probed seeds. Seed 1003 is the measured
// seed of the livelock the tap gate's satisfiability fix closed (the old
// gate re-tapped Ugin, Eye of the Storms' repeatable [0] toward a green
// card's pip forever, one intent per cycle, and the game never left turn 8
// — the pre-fix build spun past 400,000 intents here); seed 1019 is the
// probed set's heaviest finisher (1,708 intents, 21 turns).
func TestUlalekDeckSeedsPlayThrough(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck0 := testutil.RepoDeck(t, reg, "ulalek-eldrazi")
	deck1 := testutil.RepoDeck(t, reg, "valgavoth-endless-punishment")
	cmdr0 := testutil.RepoDeckFile(t, "ulalek-eldrazi").CommanderIndex()
	cmdr1 := testutil.RepoDeckFile(t, "valgavoth-endless-punishment").CommanderIndex()
	for _, seed := range []uint64{1003, 1019} {
		cfg := Config{Seed: seed, Names: []string{"ulalek-eldrazi", "valgavoth-endless-punishment"},
			Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens, Format: FormatCommander,
			StartingLife: 40, Commanders: [][]int{{cmdr0}, {cmdr1}}, Mulligans: 1}
		e := New(cfg)
		b := newTestBot(5000)
		e.Advance()
		n := 0
		for !e.G.Over && e.Pending() != nil && n < 400000 {
			if err := e.Submit(b.answer(e, e.Pending())); err != nil {
				t.Fatalf("seed %d intent %d: %v", seed, n, err)
			}
			n++
		}
		if !e.G.Over {
			t.Fatalf("seed %d did not finish (%d intents, turn %d)", seed, n, e.G.Turn)
		}
	}
}
