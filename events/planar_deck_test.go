package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestPlanarWalkApplyRotatesAndReveals(t *testing.T) {
	g := state.NewGame([]string{"A"})
	current := g.AddObject(nil, 0)
	next := g.AddObject(nil, 0)
	g.Obj(current.ID).Zone, g.Obj(next.ID).Zone = state.ZPlanarDeck, state.ZPlanarDeck
	g.Obj(current.ID).FaceDown = false
	g.Obj(next.ID).FaceDown = true
	g.SetZone(state.ZPlanarDeck, 0, []state.ObjID{current.ID, next.ID})
	Apply(g, Event{Kind: PlanarWalk, Player: 0})
	got := g.Zone(state.ZPlanarDeck, 0)
	if len(got) != 2 || got[0] != next.ID || got[1] != current.ID {
		t.Fatalf("Apply planar walk order=%v, want [%d %d]", got, next.ID, current.ID)
	}
	if g.Obj(next.ID).FaceDown || !g.Obj(current.ID).FaceDown {
		t.Fatalf("Apply planar walk face-down next/current=%v/%v", g.Obj(next.ID).FaceDown, g.Obj(current.ID).FaceDown)
	}
}
