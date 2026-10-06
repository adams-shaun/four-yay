package events

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// dfcFixture is a freely authored transforming double-faced card (never
// corpus text); AlternateMode:DoubleFaced is the layout the reset keys on.
func dfcFixture(t *testing.T, mode string) *cards.Card {
	t.Helper()
	src := "Name:Front\nTypes:Creature\nPT:2/2\nAlternateMode:" + mode + "\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/2\nOracle:y\n"
	c, d := cards.ParseBytes("dfc.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	return c
}

// TestTransformDFCLeavesBattlefieldFrontFace pins CR 712.8a: a transformed
// DoubleFaced permanent that leaves the battlefield is front face up in the
// next zone, while a battlefield->battlefield move (no exit) keeps the face.
func TestTransformDFCLeavesBattlefieldFrontFace(t *testing.T) {
	g, l := twoPlayer(t)
	o := g.AddObject(dfcFixture(t, "DoubleFaced"), 0)
	if o.Card.AlternateMode != "DoubleFaced" || len(o.Card.Faces) != 2 {
		t.Fatalf("fixture layout = %q faces=%d", o.Card.AlternateMode, len(o.Card.Faces))
	}
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
	Emit(g, l, Event{Kind: FlipFace, Obj: o.ID, Amount: 1})
	if o.FaceIdx != 1 || o.Face().Name != "Back" {
		t.Fatalf("precondition: FaceIdx=%d name=%q, want the back face on the battlefield", o.FaceIdx, o.Face().Name)
	}

	Emit(g, l, Event{Kind: MoveZone, Obj: o.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if o.Zone != state.ZGraveyard {
		t.Fatalf("zone = %v, want graveyard", o.Zone)
	}
	if o.FaceIdx != 0 || o.Face().Name != "Front" {
		t.Fatalf("in graveyard FaceIdx=%d name=%q, want the front face", o.FaceIdx, o.Face().Name)
	}
}

// A Flip-layout card is not a transforming DFC; its face index is not touched
// by the CR 712.8a reset.
func TestTransformDFCResetIgnoresOtherLayouts(t *testing.T) {
	g, l := twoPlayer(t)
	o := g.AddObject(dfcFixture(t, "Flip"), 0)
	o.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
	Emit(g, l, Event{Kind: FlipFace, Obj: o.ID, Amount: 1})
	if o.FaceIdx != 1 {
		t.Fatalf("precondition: FaceIdx=%d, want 1", o.FaceIdx)
	}
	Emit(g, l, Event{Kind: MoveZone, Obj: o.ID, From: state.ZBattlefield, To: state.ZGraveyard})
	if o.FaceIdx != 1 {
		t.Fatalf("Flip layout FaceIdx=%d after leaving, want it untouched (1)", o.FaceIdx)
	}
}

// TestTriggerPushFaceAmountMintsBackFaceLine: a "when this dies" line printed
// on the back face is minted from that face even though the card is front
// face up by the time the push folds; a plain Amount still reads the live face.
func TestTriggerPushFaceAmountMintsBackFaceLine(t *testing.T) {
	src := "Name:Front\nTypes:Creature\nPT:2/2\nAlternateMode:DoubleFaced\nT:Mode$ Always | Execute$ F\nSVar:F:DB$ Draw\nOracle:x\n\nALTERNATE\n\nName:Back\nTypes:Creature\nPT:3/2\nT:Mode$ Always | Execute$ B\nSVar:B:DB$ GainLife\nOracle:y\n"
	c, d := cards.ParseBytes("dfc2.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	c.Link()
	g, l := twoPlayer(t)
	o := g.AddObject(c, 0)
	front, back := c.Faces[0].Triggers[0].Effect, c.Faces[1].Triggers[0].Effect
	if front == nil || back == nil || front == back {
		t.Fatalf("precondition: want two distinct compiled trigger bodies, got %p %p", front, back)
	}
	if o.Face() != c.Faces[0] {
		t.Fatalf("precondition: object should be front face up")
	}
	top := func() *state.Object { return g.Obj(g.Stack[len(g.Stack)-1]) }

	Emit(g, l, Event{Kind: TriggerPush, Obj: o.ID, Player: 0, Amount: 0})
	if top().Ability != front {
		t.Fatalf("plain Amount minted %v, want the live (front) face's line", top().Ability)
	}
	Emit(g, l, Event{Kind: TriggerPush, Obj: o.ID, Player: 0, Amount: TriggerPushFaceAmount(1, 0)})
	if top().Ability != back {
		t.Fatalf("face-packed Amount minted %v, want the back face's line", top().Ability)
	}
	n := len(g.Stack)
	Emit(g, l, Event{Kind: TriggerPush, Obj: o.ID, Player: 0, Amount: TriggerPushFaceAmount(2, 0)})
	if len(g.Stack) != n {
		t.Fatalf("an out-of-range face minted an ability")
	}

	Emit(g, l, Event{Kind: FlipFace, Obj: o.ID, Amount: 1})
	if o.FaceIdx != 1 || o.Face().Name != "Back" {
		t.Fatalf("precondition: expected live face 1, got face %d %q", o.FaceIdx, o.Face().Name)
	}
	Emit(g, l, Event{Kind: TriggerPush, Obj: o.ID, Player: 0, Amount: TriggerPushFaceAmount(0, 0)})
	if top().Ability != front {
		t.Fatalf("face-0 amount minted %v, want front-face line %p while face 1 is live", top().Ability, front)
	}
	Emit(g, l, Event{Kind: TriggerPush, Obj: o.ID, Player: 0, Amount: 0})
	if top().Ability != back {
		t.Fatalf("plain live-face amount minted %v, want back-face line %p", top().Ability, back)
	}
}
