package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestOzaiUnspentManaBecomesRedAtBoundary: with Ozai as the single carrier,
// unspent mana of any colour turns red at the step boundary instead of
// emptying (the opponent's pool still empties).
func TestOzaiUnspentManaBecomesRedAtBoundary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := realCardEngine(t, reg, 8675312, "Ozai, the Phoenix King")
	if o := e.G.Obj(ids[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("Ozai must be on the battlefield before the boundary")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 2, Counter: "G"})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Amount: 1, Counter: "U"})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Amount: 1, Counter: "G"})
	if p := e.G.Players[0].Pool; p[state.MG] != 2 || p[state.MU] != 1 || p[state.MR] != 0 {
		t.Fatalf("pool precondition not established: %v", p)
	}
	e.finishStepBoundary(state.StepMain1, state.StepBeginCombat)
	if got := e.G.Players[0].Pool; got[state.MR] != 3 || got.Total() != 3 {
		t.Fatalf("Ozai pool after boundary = %v, want three red", got)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("opponent's pool = %d, want cleared", got)
	}
	replayCheck(t, e, cfg)
}

func TestLoseManaAndConniveHeadsRegistered(t *testing.T) {
	for _, h := range []string{"repl:Connive", "repl:LoseMana"} {
		if !effects.Supported()[h] {
			t.Errorf("%s is not declared supported", h)
		}
	}
}
