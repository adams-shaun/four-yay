package events

// CR 720's ControlPlayerChange fold: a +1 grant records the controlling seat
// and the turn it was armed on, a -1 expiry removes both, and a self-grant is
// ignored. Replay re-derives the same interval from the same events.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestControlPlayerChangeFoldsGrantExpiryAndSelf(t *testing.T) {
	g, _ := twoPlayer(t)
	Apply(g, Event{Kind: TurnChange, Player: 0, Amount: 5})
	if g.Turn != 5 {
		t.Fatalf("setup: turn %d, want 5", g.Turn)
	}
	Apply(g, Event{Kind: ControlPlayerChange, Player: 0,
		IDs: []state.ObjID{state.PlayerRef(1)}, Amount: 1})
	if ctl := g.ControlledBy[1]; ctl != 0 {
		t.Fatalf("after grant ControlledBy = %+v, want 1 -> 0", g.ControlledBy)
	}
	if g.ControlArmedTurn[1] != 5 {
		t.Fatalf("armed turn = %d, want 5", g.ControlArmedTurn[1])
	}
	Apply(g, Event{Kind: ControlPlayerChange, Player: 0,
		IDs: []state.ObjID{state.PlayerRef(1)}, Amount: -1})
	if _, ok := g.ControlledBy[1]; ok {
		t.Fatalf("after expiry ControlledBy = %+v", g.ControlledBy)
	}
	if _, ok := g.ControlArmedTurn[1]; ok {
		t.Fatalf("after expiry ControlArmedTurn = %+v", g.ControlArmedTurn)
	}
	// CR 720.6: a player cannot control themselves.
	Apply(g, Event{Kind: ControlPlayerChange, Player: 0,
		IDs: []state.ObjID{state.PlayerRef(0)}, Amount: 1})
	if len(g.ControlledBy) != 0 {
		t.Fatalf("self-grant folded: %+v", g.ControlledBy)
	}
}
