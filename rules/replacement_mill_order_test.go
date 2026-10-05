package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMillReplacementOrderComposesBothWays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name  string
		first string
		want  int32
	}{
		{"Water Crystal then Bruvac", "water", 12},
		{"Bruvac then Water Crystal", "bruvac", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := millTriggerEngine(t)
			waterID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "The Water Crystal"))
			bruvacID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Bruvac the Grandiloquent"))
			water := e.G.Obj(waterID)
			bruvac := e.G.Obj(bruvacID)
			if water == nil || water.Zone != state.ZBattlefield || bruvac == nil || bruvac.Zone != state.ZBattlefield {
				t.Fatal("precondition: both replacement sources must be on the battlefield")
			}
			matches := []replMatch{{id: waterID, repl: &water.Face().Repls[0]}, {id: bruvacID, repl: &bruvac.Face().Repls[0]}}
			ev := events.Event{Kind: events.MillProposal, Player: 1, Amount: 2}
			d := millReplacementDecision(e.G, ev, matches, []int{0, 1})
			if len(d.Options) != 2 || d.Options[0].Obj != waterID || d.Options[1].Obj != bruvacID {
				t.Fatalf("replacement choice options = %+v, want both active sources", d.Options)
			}
			order := []int{0, 1}
			if tc.first == "bruvac" {
				order = []int{1, 0}
			}
			for _, i := range order {
				var handled bool
				ev, handled = e.continueMillReplacements(ev, []replMatch{matches[i]})
				if !handled {
					t.Fatal("Mill proposal was not held for replacement")
				}
			}
			if ev.Amount != tc.want {
				t.Fatalf("rewritten mill count = %d, want %d", ev.Amount, tc.want)
			}
		})
	}
}
