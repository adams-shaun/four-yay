package view

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestCardViewProjectsTokenAndCopyFlags pins the projection of
// state.Object.IsToken/IsCopy into view.CardView (MBX-1): a battlefield
// token, a battlefield copy that is not a token, and a token copy (which
// carries BOTH, CR 706.2) each project their own flag, and an ordinary
// nontoken card projects neither. Before this projection the ManaBrew wire
// derived isToken from CardView.Token -- the "#<id>" display tag every card
// carries -- so every card went out marked as a token.
func TestCardViewProjectsTokenAndCopyFlags(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	bear, d := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	bear.Link()

	// Each object's flags and zone are set immediately after its AddObject,
	// BEFORE the next one: AddObject returns a pointer into g.Objs that the
	// next append can reallocate away (the same discipline
	// ephemeral_test.go follows).
	plain := g.AddObject(bear, 0)
	plain.Zone = state.ZBattlefield
	tok := g.AddObject(bear, 0)
	tok.IsToken = true
	tok.Zone = state.ZBattlefield
	copyOnly := g.AddObject(bear, 0)
	copyOnly.IsCopy = true
	copyOnly.Zone = state.ZBattlefield
	copyTok := g.AddObject(bear, 0)
	copyTok.IsToken = true
	copyTok.IsCopy = true
	copyTok.Zone = state.ZBattlefield
	// Precondition: the state objects really do carry the shapes the
	// projection is asserted against.
	if tok.IsToken == plain.IsToken || copyTok.IsCopy == plain.IsCopy {
		t.Fatal("fixture objects must differ in their token/copy flags")
	}
	g.SetZone(state.ZBattlefield, 0, []state.ObjID{plain.ID, tok.ID, copyOnly.ID, copyTok.ID})

	v := Project(g, nil, 0, nil)
	byID := map[state.ObjID]CardView{}
	for _, c := range v.Players[0].Battlefield {
		byID[c.ID] = c
	}
	if len(byID) != 4 {
		t.Fatalf("battlefield = %+v, want all four objects projected", v.Players[0].Battlefield)
	}
	if byID[plain.ID].IsToken || byID[plain.ID].IsCopy {
		t.Fatalf("nontoken card projected a token/copy flag: %+v", byID[plain.ID])
	}
	if !byID[tok.ID].IsToken || byID[tok.ID].IsCopy {
		t.Fatalf("token projected isToken=%v isCopy=%v, want true/false", byID[tok.ID].IsToken, byID[tok.ID].IsCopy)
	}
	if byID[copyOnly.ID].IsToken || !byID[copyOnly.ID].IsCopy {
		t.Fatalf("copy projected isToken=%v isCopy=%v, want false/true", byID[copyOnly.ID].IsToken, byID[copyOnly.ID].IsCopy)
	}
	if !byID[copyTok.ID].IsToken || !byID[copyTok.ID].IsCopy {
		t.Fatalf("token copy projected isToken=%v isCopy=%v, want true/true", byID[copyTok.ID].IsToken, byID[copyTok.ID].IsCopy)
	}
}

// TestFaceDownProjectionDoesNotRevealTokenFlag pins the other half: the
// face-down replacement rebuilds the CardView from a fresh literal, so a
// card hidden behind its face reveals no token/copy flag to a viewer who
// may not look -- the flag set on the object before projection stays behind
// with the full card.
func TestFaceDownProjectionDoesNotRevealTokenFlag(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	bear, d := cards.ParseBytes("b.txt", []byte("Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	bear.Link()

	fd := g.AddObject(bear, 1)
	fd.IsToken = true
	fd.FaceDown = true
	fd.Zone = state.ZBattlefield
	if !fd.IsToken {
		t.Fatal("fixture object must carry IsToken before projection")
	}
	fd.Zone = state.ZBattlefield
	g.SetZone(state.ZBattlefield, 1, []state.ObjID{fd.ID})

	v := Project(g, nil, 0, nil)
	bf := v.Players[1].Battlefield
	if len(bf) != 1 || !bf[0].FaceDown || bf[0].Name != "" {
		t.Fatalf("face-down projection = %+v, want a redacted FaceDown entry", bf)
	}
	if bf[0].IsToken || bf[0].IsCopy {
		t.Fatalf("face-down card revealed token/copy flags to a viewer who may not look: %+v", bf[0])
	}
}

// TestStackCopyProjectsIsCopy covers the copy flag's other home: a copy on
// the stack is a public fact (CR 707.10), and the stack spell's embedded
// CardView carries it like a battlefield copy does.
func TestStackCopyProjectsIsCopy(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	bolt, d := cards.ParseBytes("b.txt", []byte("Name:Bolt\nManaCost:R\nTypes:Instant\nOracle:x\nA:SP$ DealDamage\n"))
	if len(d) != 0 {
		t.Fatalf("diags: %v", d)
	}
	bolt.Link()

	cp := g.AddObject(bolt, 0)
	cp.IsCopy = true
	cp.Zone = state.ZStack
	g.SetZone(state.ZStack, 0, []state.ObjID{cp.ID})
	if !cp.IsCopy {
		t.Fatal("fixture stack object must carry IsCopy")
	}

	v := Project(g, nil, 0, nil)
	if len(v.Stack) != 1 || v.Stack[0].Card == nil {
		t.Fatalf("stack = %+v, want one spell with an embedded card view", v.Stack)
	}
	if !v.Stack[0].Card.IsCopy || v.Stack[0].Card.IsToken {
		t.Fatalf("stack copy projected isToken=%v isCopy=%v, want false/true",
			v.Stack[0].Card.IsToken, v.Stack[0].Card.IsCopy)
	}
}
