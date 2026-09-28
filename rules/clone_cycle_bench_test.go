package rules

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// cloneCycleRoot plays uw-tempo against opp with the default
// bot from genesis to the first decision at turn >= minTurn and returns that
// mid-game engine: the shape of an AlphaZero search root, whose event log and
// object arena are thousands of entries long.
func cloneCycleRoot(b *testing.B, opp string, minTurn int32) *Engine {
	b.Helper()
	reg := testutil.CorpusRegistry(b)
	da := testutil.RepoDeck(b, reg, "uw-tempo")
	db := testutil.RepoDeck(b, reg, opp)
	cfg := Config{Seed: 90000000, Names: []string{"uw-tempo", opp},
		Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	bot := newTestBot(7)
	for n := 0; !e.G.Over && e.Pending() != nil && e.G.Turn < minTurn; n++ {
		if n > 20000 {
			b.Fatal("root game did not reach the target turn")
		}
		if err := e.Submit(bot.answer(e, e.Pending())); err != nil {
			b.Fatal(err)
		}
	}
	if e.G.Over || e.Pending() == nil {
		b.Fatalf("root game ended before turn %d", minTurn)
	}
	return e
}

// cloneCycleSteps is the number of intents one simulation plays on its clone
// (the az search plays ~35-55 per simulation).
const cloneCycleSteps = 40

// benchCloneCycle reproduces the AlphaZero simulation cycle: clone a mid-game
// root, then step the clone cloneCycleSteps intents with the default bot.
// One op is one simulation.
func benchCloneCycle(b *testing.B, opp string, minTurn int32, hypothetical bool) {
	benchWithoutVerify(b)
	root := cloneCycleRoot(b, opp, minTurn)
	b.Logf("root: turn %d, %d events, %d objects", root.G.Turn, len(root.L.Events), len(root.G.Objs))
	b.ReportAllocs()
	b.ResetTimer()
	appended, added := 0, 0
	board := botpolicy.NewBoard(2)
	defer func() {
		b.ReportMetric(float64(appended)/float64(b.N), "events/op")
		b.ReportMetric(float64(added)/float64(b.N), "objs/op")
	}()
	for i := 0; i < b.N; i++ {
		var c *Engine
		if hypothetical {
			c = root.CloneHypothetical(uint64(i))
		} else {
			c = root.Clone()
		}
		r := rand.New(rand.NewPCG(uint64(i), 1))
		for s := 0; s < cloneCycleSteps && !c.G.Over && c.Pending() != nil; s++ {
			d := c.Pending()
			// One reused Board, as the search seat keeps (BoardFromGameInto),
			// so the harness's own map churn does not swamp the engine's.
			in := botpolicy.Decide(botpolicy.BoardFromGameInto(c.G, c, d.Player, &board), d, r)
			var err error
			if hypothetical {
				err = c.SubmitHypothetical(in)
			} else {
				err = c.Submit(in)
			}
			if err != nil {
				b.Fatal(err)
			}
		}
		appended += len(c.L.Events) - len(root.L.Events)
		added += len(c.G.Objs) - len(root.G.Objs)
	}
}

func BenchmarkCloneCycle(b *testing.B) {
	for _, c := range cloneCycleRoots {
		b.Run(fmt.Sprintf("%s-t%d", c.opp, c.turn), func(b *testing.B) { benchCloneCycle(b, c.opp, c.turn, false) })
	}
}

// cloneCycleRoots are the benchmark's search roots: a mid-game and a late
// position (the late one has the long log a t13+ search clones).
var cloneCycleRoots = []struct {
	opp  string
	turn int32
}{{"mono-white-equipment", 8}, {"mono-blue-tempo", 16}}

func BenchmarkCloneCycleHypothetical(b *testing.B) {
	c := cloneCycleRoots[len(cloneCycleRoots)-1]
	benchCloneCycle(b, c.opp, c.turn, true)
}

// BenchmarkCloneOnly prices Engine.Clone alone at the same root.
func BenchmarkCloneOnly(b *testing.B) {
	benchWithoutVerify(b)
	c := cloneCycleRoots[len(cloneCycleRoots)-1]
	root := cloneCycleRoot(b, c.opp, c.turn)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		newEngineSink = root.Clone()
	}
}
