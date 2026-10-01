package azmcts

import (
	"math"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestHeuristicLeafMatchesTheView pins heuristicLeafValue to the value it
// replaces -- searchprobe.LeafValue over the actor's projected view -- bit
// for bit, for both seats at every decision of several whole bot games, and
// for face-down permanents (hidden from the opponent, visible to their
// controller).
func TestHeuristicLeafMatchesTheView(t *testing.T) {
	viaView := func(e *rules.Engine, actor state.PlayerID) float64 {
		return searchprobe.LeafValue(view.Project(e.G, e, actor, e.Pending()), actor)
	}
	check := func(e *rules.Engine, where string) {
		t.Helper()
		for actor := state.PlayerID(0); int(actor) < len(e.G.Players); actor++ {
			got, want := heuristicLeafValue(e, actor), viaView(e, actor)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("%s, actor %d: direct leaf %v, view leaf %v", where, actor, got, want)
			}
		}
	}
	pairs := [][2]string{{"uw-tempo", "mono-red-prowess"}, {"mono-white-equipment", "mono-green-stompy"}, {"mono-black-aggro", "mono-blue-tempo"}}
	checked := 0
	for gi, pr := range pairs {
		cfg := testConfig(t, pr[0], pr[1], testSeed+uint64(gi))
		e := rules.New(cfg)
		e.Advance()
		rngs := searchprobe.BotRandoms(cfg.Seed, 2)
		board := botpolicy.NewBoard(2)
		for step := 0; step < 3000 && !e.G.Over; step++ {
			d := e.Pending()
			if d == nil {
				break
			}
			check(e, pr[0]+" vs "+pr[1])
			checked++
			if step%40 == 0 {
				// Face-down permanents: redacted for the opponent, read by the
				// controller. A test-only write on a throwaway clone.
				c := e.Clone()
				for _, id := range c.G.Zone(state.ZBattlefield, 0) {
					c.G.Obj(id).FaceDown = true
				}
				check(c, "face-down clone")
			}
			in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
			if err := e.Submit(in); err != nil {
				t.Fatal(err)
			}
		}
		check(e, "final")
	}
	if checked < 500 {
		t.Fatalf("precondition: only %d positions checked", checked)
	}
}
