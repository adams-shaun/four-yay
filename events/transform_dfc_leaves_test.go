package events

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestTransformDFCLeavesBattlefieldFrontFace pins CR 712.8a: a transformed
// double-faced card leaving the battlefield is front face up in the new zone.
func TestTransformDFCLeavesBattlefieldFrontFace(t *testing.T) {
	src := "Name:Front\nTypes:Creature\nPT:1/1\nAlternateMode:DoubleFaced\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/3\nOracle:x\n"
	c, d := cards.ParseBytes("t.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	if c.AlternateMode != "DoubleFaced" || len(c.Faces) != 2 {
		t.Fatalf("fixture is not a DoubleFaced two-face card: %q, %d faces", c.AlternateMode, len(c.Faces))
	}

	g, l := twoPlayer(t)
	o := g.AddObject(c, 0)
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
	Emit(g, l, Event{Kind: FlipFace, Obj: o.ID, Amount: 1})
	if o.FaceIdx != 1 {
		t.Fatalf("precondition: FaceIdx = %d on the battlefield, want 1", o.FaceIdx)
	}
	Emit(g, l, Event{Kind: MoveZone, Obj: o.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if o.Zone != state.ZGraveyard {
		t.Fatalf("object is in zone %v, want the graveyard", o.Zone)
	}
	if o.FaceIdx != 0 {
		t.Errorf("FaceIdx = %d in the graveyard, want 0 (front face)", o.FaceIdx)
	}
}
