package azmcts

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestHeuristicLeafCharsKeepsLeafValue pins the lean heuristic leaf: at every
// decision of whole bot games, for both seats, leafValue through
// heuristicLeafChars and one reused view is exactly searchprobe.LeafValue on
// the full fresh projection the leaf used to build. If LeafValue ever reads a
// fact heuristicLeafChars suppresses, this fails.
func TestHeuristicLeafCharsKeepsLeafValue(t *testing.T) {
	sc := new(viewScratch)
	checked := 0
	for _, pair := range [][2]string{{"mono-red-prowess", "mono-blue-tempo"}, {"mono-blue-tempo", "mono-red-prowess"}} {
		cfg := testConfig(t, pair[0], pair[1], testSeed)
		e := rules.New(cfg)
		e.Advance()
		rngs := searchprobe.BotRandoms(cfg.Seed, 2)
		board := botpolicy.NewBoard(2)
		for steps := 0; steps < 3000 && !e.G.Over; steps++ {
			d := e.Pending()
			if d == nil {
				t.Fatalf("no pending decision at step %d", steps)
			}
			for actor := state.PlayerID(0); actor < 2; actor++ {
				want := searchprobe.LeafValue(view.Project(e.G, e, actor, e.Pending()), actor)
				if got := leafValue(nil, e, actor, sc); got != want {
					t.Fatalf("%v step %d seat %d: lean leaf %v, full projection %v", pair, steps, actor, got, want)
				}
				checked++
			}
			in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
			if err := e.Submit(in); err != nil {
				t.Fatalf("step %d submit: %v", steps, err)
			}
		}
	}
	if checked < 200 {
		t.Fatalf("only %d leaves compared", checked)
	}
}
