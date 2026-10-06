package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// castBattlefieldGame seats a spell on the stack and one permanent on the
// battlefield (both Bears), returning the game, the spell and the permanent.
func castBattlefieldGame(t *testing.T) (*state.Game, state.ObjID, state.ObjID) {
	t.Helper()
	g, spell := gameWithOneCard(t)
	perm := g.AddObject(g.Obj(spell).Card, 0).ID
	g.SetZone(state.ZLibrary, 0, []state.ObjID{perm})
	g.Obj(perm).Zone = state.ZLibrary
	Apply(g, Event{Kind: MoveZone, Obj: perm, From: state.ZLibrary, To: state.ZBattlefield})
	if g.Obj(perm).Zone != state.ZBattlefield {
		t.Fatal("precondition: the permanent is not on the battlefield")
	}
	Apply(g, Event{Kind: PutOnStack, Obj: spell, Player: 0, From: state.ZHand, To: state.ZStack})
	if g.Obj(spell).Zone != state.ZStack {
		t.Fatal("precondition: the spell is not on the stack")
	}
	return g, spell, perm
}

// TestCastBattlefieldSnapshotFoldFreezesAndOutlivesDeparture: the fold freezes
// the permanent's facts and derived P/T; the permanent leaving afterwards does
// not touch them; a stack copy shares them; leaving the stack clears them.
func TestCastBattlefieldSnapshotFoldFreezesAndOutlivesDeparture(t *testing.T) {
	g, spell, perm := castBattlefieldGame(t)
	if g.Obj(spell).CastBattlefield != nil {
		t.Fatal("precondition: a spell starts with no snapshot")
	}
	Apply(g, Event{Kind: CastBattlefield, Obj: spell, Player: 0,
		IDs: []state.ObjID{perm}, Pairs: [][2]state.ObjID{{5, 7}}})
	snap := g.Obj(spell).CastBattlefield
	if snap == nil {
		t.Fatal("the fold did not freeze a battlefield")
	}
	if p, tough, ok := snap.DerivedPT(perm); !ok || p != 5 || tough != 7 {
		t.Fatalf("frozen P/T = %d/%d (ok %v), want 5/7", p, tough, ok)
	}
	Apply(g, Event{Kind: MoveZone, Obj: perm, From: state.ZBattlefield, To: state.ZGraveyard})
	if g.Obj(perm).Zone != state.ZGraveyard {
		t.Fatal("precondition: the permanent did not leave")
	}
	fo := snap.Game.Obj(perm)
	if fo == nil || fo.Zone != state.ZBattlefield || fo.Controller != 0 || len(snap.Game.Zone(state.ZBattlefield, 0)) != 1 {
		t.Fatalf("frozen permanent changed with the live one: %+v", fo)
	}
	Apply(g, Event{Kind: StackCopy, Obj: spell, Player: 0})
	if cp := g.Obj(g.Stack[len(g.Stack)-1]); cp == nil || !cp.IsCopy || cp.CastBattlefield != snap {
		t.Fatalf("the copy does not keep the original's snapshot: %+v", cp)
	}
	Apply(g, Event{Kind: MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard})
	if g.Obj(spell).CastBattlefield != nil {
		t.Fatal("a spell off the stack kept its snapshot")
	}
}

// TestCastBattlefieldSnapshotFoldEmptyAndOffStack: an event with no IDs freezes
// a present-but-empty battlefield (authoritative zero, not "missing"); an
// event for an object not on the stack freezes nothing.
func TestCastBattlefieldSnapshotFoldEmptyAndOffStack(t *testing.T) {
	g, spell, perm := castBattlefieldGame(t)
	Apply(g, Event{Kind: CastBattlefield, Obj: spell, Player: 0})
	snap := g.Obj(spell).CastBattlefield
	if snap == nil {
		t.Fatal("an empty freeze must still be present")
	}
	if n := len(snap.Game.Zone(state.ZBattlefield, 0)) + len(snap.Game.Zone(state.ZBattlefield, 1)); n != 0 || len(snap.PT) != 0 {
		t.Fatalf("empty freeze holds %d permanents / %d P/T", n, len(snap.PT))
	}
	Apply(g, Event{Kind: CastBattlefield, Obj: perm, Player: 0})
	if g.Obj(perm).CastBattlefield != nil {
		t.Fatal("a battlefield permanent was given an as-cast snapshot")
	}
}
