package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// ManaUndo (announce-then-pay spec §5) removes exactly the units one ManaAdd
// credited -- the plain slot, or the slot and snow tally of an S<colour>
// counter -- and untaps its named source.
func TestManaUndoReversesOneManaAdd(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	land := g.AddObject(bearCard(), 0)
	land.Zone = state.ZBattlefield
	Apply(g, Event{Kind: Tap, Obj: land.ID})
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "B", Amount: 1})
	Apply(g, Event{Kind: ManaAdd, Player: 0, Counter: "SU", Amount: 2})
	Apply(g, Event{Kind: ManaUndo, Player: 0, Obj: land.ID, Counter: "B", Amount: 1})
	if land.Tapped || g.Players[0].Pool[state.ManaIndex('B')] != 0 {
		t.Fatalf("after plain undo: tapped=%v pool=%v", land.Tapped, g.Players[0].Pool)
	}
	Apply(g, Event{Kind: ManaUndo, Player: 0, Counter: "SU", Amount: 2})
	if g.Players[0].Pool.Total() != 0 || g.Players[0].Snow.Total() != 0 {
		t.Fatalf("after snow undo: pool=%v snow=%v", g.Players[0].Pool, g.Players[0].Snow)
	}
	// An over-long undo never drives the pool negative.
	Apply(g, Event{Kind: ManaUndo, Player: 0, Counter: "G", Amount: 3})
	if g.Players[0].Pool[state.ManaIndex('G')] != 0 {
		t.Fatalf("undo drove the pool negative: %v", g.Players[0].Pool)
	}
	if ManaUndo.String() != "mana_undo" {
		t.Fatalf("ManaUndo name = %q", ManaUndo.String())
	}
}
