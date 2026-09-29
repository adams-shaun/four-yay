package view

// CR 723.4 conformance for the "also visible" widening: while one player
// controls another (a player-controlling effect created by api:ControlPlayer),
// information visible to the controlled player is visible to the controller
// too -- specifically that player's hand and the face of any face-down
// creatures they control. view.ProjectForControlled carries the controlled
// seats as an explicit, narrow set; these tests pin both the positive
// (the controller reads the controlled seat's hand and face-down faces) and
// the leak invariant (a THIRD seat's hand stays hidden).

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestControllerSeesControlledHandNotThirdSeat is the CR 723.4 positive plus
// the leak invariant in one board: viewer 0 controls seat 1, so seat 1's hand
// is populated and seat 2's stays nil.
func TestControllerSeesControlledHandNotThirdSeat(t *testing.T) {
	g := fourSeatBoard(t)
	// Precondition: the board really has distinct hands to distinguish, or the
	// positive assertion below would pass on an empty hand of the wrong shape.
	if len(g.Zone(state.ZHand, 1)) == 0 || len(g.Zone(state.ZHand, 2)) == 0 {
		t.Fatalf("precondition: seats need hands, got seat1=%d seat2=%d",
			len(g.Zone(state.ZHand, 1)), len(g.Zone(state.ZHand, 2)))
	}
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}

	v := ProjectForControlled(g, flatChars{g}, 0, Seat, []state.PlayerID{1}, nil)
	var pv0, pv1, pv2 *PlayerView
	for i := range v.Players {
		switch v.Players[i].ID {
		case 0:
			pv0 = &v.Players[i]
		case 1:
			pv1 = &v.Players[i]
		case 2:
			pv2 = &v.Players[i]
		}
	}
	if pv0 == nil || pv1 == nil || pv2 == nil {
		t.Fatalf("expected seats 0,1,2 in the view: %+v", v.Players)
	}
	// CR 723.4: the controller reads the controlled player's hand.
	if len(pv1.Hand) != 3 {
		t.Fatalf("controller cannot see controlled seat 1's hand: %+v", pv1.Hand)
	}
	if pv1.HandSize != 3 {
		t.Fatalf("controlled seat 1 hand size = %d, want 3", pv1.HandSize)
	}
	// Leak invariant: seat 2 is not controlled, its hand stays hidden.
	if pv2.Hand != nil {
		t.Fatalf("viewer 0 sees uninvolved seat 2's hand: %+v", pv2.Hand)
	}
	if pv2.HandSize != 3 {
		t.Fatalf("uninvolved seat 2 hand size = %d, want the public 3", pv2.HandSize)
	}
	// A controller still reads its own hand (the gate is widened, not moved).
	if len(pv0.Hand) != 3 {
		t.Fatalf("viewer 0 lost its own hand: %+v", pv0.Hand)
	}
}

// TestOrdinaryProjectionStillHidesAControlledSeat pins that the widening is
// opt-in: ProjectFor/Project (the nil set) do not read ControlledBy on their
// own, so no existing caller changed behaviour.
func TestOrdinaryProjectionStillHidesAControlledSeat(t *testing.T) {
	g := fourSeatBoard(t)
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}
	v := Project(g, flatChars{g}, 0, nil)
	for _, pv := range v.Players {
		if pv.ID == 0 {
			if len(pv.Hand) != 3 {
				t.Fatalf("viewer 0 cannot see own hand")
			}
			continue
		}
		if pv.Hand != nil {
			t.Fatalf("plain Project revealed seat %d's hand: %+v", pv.ID, pv.Hand)
		}
	}
}

// TestControllerSeesControlledFaceDownFace pins the second half of the CR
// 723.4 example ("the face of any face-down creatures they control") and the
// matching leak: only the controlled seat's face-down face is revealed.
func TestControllerSeesControlledFaceDownFace(t *testing.T) {
	g := fourSeatBoard(t)
	// A face-down permanent on seat 1's battlefield (controlled by seat 1) and
	// one on seat 2's, so the reveal can be shown to be scoped to the
	// controlled seat rather than blanket.
	fd1 := g.Zone(state.ZBattlefield, 1)[0]
	fd2 := g.Zone(state.ZBattlefield, 2)[0]
	o1 := g.Obj(fd1)
	o2 := g.Obj(fd2)
	o1.FaceDown = true
	o1.Controller = 1
	o2.FaceDown = true
	o2.Controller = 2
	// Precondition: the objects are on the battlefield, face down, under the
	// right controller -- a hand-only or face-up object would make the
	// assertions below pass for the wrong reason.
	if o1.Zone != state.ZBattlefield || !o1.FaceDown || o1.Controller != 1 {
		t.Fatalf("precondition: seat 1 face-down permanent = %+v", o1)
	}
	if o2.Zone != state.ZBattlefield || !o2.FaceDown || o2.Controller != 2 {
		t.Fatalf("precondition: seat 2 face-down permanent = %+v", o2)
	}
	g.ControlledBy = map[state.PlayerID]state.PlayerID{1: 0}

	v := ProjectForControlled(g, flatChars{g}, 0, Seat, []state.PlayerID{1}, nil)
	faceOf := func(pv PlayerView, id state.ObjID) *CardView {
		for i := range pv.Battlefield {
			if pv.Battlefield[i].ID == id {
				return &pv.Battlefield[i]
			}
		}
		return nil
	}
	for _, pv := range v.Players {
		if pv.ID != 1 && pv.ID != 2 {
			continue
		}
		if pv.ID == 1 {
			cv := faceOf(pv, fd1)
			if cv == nil || !cv.FaceDown {
				t.Fatalf("controlled seat 1's face-down permanent missing/marked up: %+v", cv)
			}
			if cv.Name == "" {
				t.Fatalf("controller cannot read controlled seat 1's face-down face: %+v", cv)
			}
			continue
		}
		cv := faceOf(pv, fd2)
		if cv == nil || !cv.FaceDown {
			t.Fatalf("uninvolved seat 2's face-down permanent missing: %+v", cv)
		}
		if cv.Name != "" {
			t.Fatalf("controller reads uninvolved seat 2's face-down face: %+v", cv)
		}
	}
}
