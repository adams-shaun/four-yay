package state

import "testing"

// TestCastBattlefieldSnapshotClonePreservesAndShares pins the carrier's clone
// contract (state.Object.CastBattlefield, folded by events.Apply's
// CastBattlefield kind and read by Count$LastStateBattlefieldWithFallback).
//
// The snapshot is IMMUTABLE once FreezeBattlefield returns, so Object.CloneDeep
// and the arena-backed Game.Clone share the one pointer rather than copying the
// frozen game; this test proves both clone paths carry it, that a nil (absent)
// snapshot stays nil while a non-nil empty one stays present, and that the
// clone's snapshot is the same immutable value -- there is no mutable field for
// either game to alias.
func TestCastBattlefieldSnapshotClonePreservesAndShares(t *testing.T) {
	g := NewGame([]string{"a", "b"})
	perm := g.AddObject(nil, 0).ID
	g.SetZone(ZBattlefield, 0, []ObjID{perm})
	g.Obj(perm).Zone = ZBattlefield
	spell := g.AddObject(nil, 0).ID
	g.SetZone(ZStack, 0, []ObjID{spell})
	g.Obj(spell).Zone = ZStack

	// Precondition: a fresh spell carries no snapshot.
	if g.Obj(spell).CastBattlefield != nil {
		t.Fatal("precondition: the spell already carries a snapshot")
	}

	snap := FreezeBattlefield(g, []ObjID{perm}, []FrozenPT{{ID: perm, Power: 3, Toughness: 4}})
	g.Obj(spell).CastBattlefield = snap

	// Precondition: the freeze actually captured the permanent and its P/T.
	if p, tough, ok := snap.DerivedPT(perm); !ok || p != 3 || tough != 4 {
		t.Fatalf("precondition: frozen P/T = %d/%d (ok %v), want 3/4", p, tough, ok)
	}

	// Object.CloneDeep (the ordinary clone) and Game.Clone (the arena-backed
	// clone) must both carry the snapshot.
	objClone := g.Obj(spell).CloneDeep()
	if objClone.CastBattlefield != snap {
		t.Fatalf("Object.CloneDeep carries %p, want the shared %p", objClone.CastBattlefield, snap)
	}
	gClone := g.Clone()
	if gClone.Obj(spell).CastBattlefield != snap {
		t.Fatalf("Game.Clone carries %p, want the shared %p", gClone.Obj(spell).CastBattlefield, snap)
	}

	// The frozen game is independent of the live one: moving the live
	// permanent off the battlefield and moving the live spell off the stack
	// must not touch the frozen copy or the clone's snapshot.
	g.Obj(perm).Zone = ZGraveyard
	g.Obj(spell).Zone = ZGraveyard
	g.Obj(spell).CastBattlefield = nil
	if fo := snap.Game.Obj(perm); fo == nil || fo.Zone != ZBattlefield {
		t.Fatalf("frozen permanent changed with the live one: %+v", fo)
	}
	if p, tough, ok := snap.DerivedPT(perm); !ok || p != 3 || tough != 4 {
		t.Fatalf("frozen P/T changed to %d/%d (ok %v)", p, tough, ok)
	}
	if gClone.Obj(spell).CastBattlefield != snap {
		t.Fatal("clearing the live snapshot disturbed the clone's")
	}

	// A present-but-empty snapshot is authoritative zero and is NOT the same
	// as an absent (nil) one; both clone paths keep the distinction.
	empty := FreezeBattlefield(g, nil, nil)
	if empty == nil {
		t.Fatal("a freeze with no ids must still be present")
	}
	if n := len(empty.Game.Zone(ZBattlefield, 0)) + len(empty.Game.Zone(ZBattlefield, 1)); n != 0 {
		t.Fatalf("empty freeze holds %d permanents, want 0", n)
	}
	present := Object{ID: 5, CastBattlefield: empty}
	if c := present.CloneDeep(); c.CastBattlefield == nil {
		t.Fatal("CloneDeep turned a present-but-empty snapshot into nil")
	}
	absent := Object{ID: 6}
	if c := absent.CloneDeep(); c.CastBattlefield != nil {
		t.Fatalf("CloneDeep invented a snapshot: %v", c.CastBattlefield)
	}
}
