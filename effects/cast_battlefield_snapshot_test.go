package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// bfItoa is a tiny base-10 int formatter (strconv is avoided in this package by
// convention).
func bfItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// castBattlefieldSnapshotGame seats n 1/2 Bears on player 0's battlefield and
// one spell on the stack, returning the game, the spell id and the Bear ids.
func castBattlefieldSnapshotGame(t *testing.T, bears int) (*state.Game, state.ObjID, []state.ObjID) {
	t.Helper()
	g := state.NewGame([]string{"you", "them"})
	var ids []state.ObjID
	for i := 0; i < bears; i++ {
		src := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:1/2\nOracle:x\n"
		c, d := cards.ParseBytes("t.txt", []byte(src))
		if len(d) != 0 {
			t.Fatalf("diags: %v", d)
		}
		c.Link()
		for _, f := range c.Faces {
			f.ApplyIntrinsics()
		}
		o := g.AddObject(c, 0)
		o.Zone = state.ZBattlefield
		g.SetZone(state.ZBattlefield, 0, append(g.Zone(state.ZBattlefield, 0), o.ID))
		ids = append(ids, o.ID)
	}
	c, _ := cards.ParseBytes("t.txt", []byte("Name:Spell\nManaCost:1 G\nTypes:Sorcery\nOracle:x\n"))
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	spell := g.AddObject(c, 0).ID
	g.Obj(spell).Zone = state.ZStack
	g.SetZone(state.ZStack, 0, append(g.Zone(state.ZStack, 0), spell))
	return g, spell, ids
}

// TestCastBattlefieldSnapshotHeadFrozenVsLive is the effects-level contract of
// Count$LastStateBattlefieldWithFallback: the frozen as-cast battlefield is
// authoritative when present (including empty), the live battlefield is the
// fallback only when the source carries no snapshot, and the frozen DERIVED
// power answers a $GreatestCardPower suffix from the captured values rather
// than the live characteristics.
func TestCastBattlefieldSnapshotHeadFrozenVsLive(t *testing.T) {
	g, spell, ids := castBattlefieldSnapshotGame(t, 3)
	if len(ids) != 3 {
		t.Fatalf("precondition: %d Bears, want 3", len(ids))
	}
	h := &fakeHost{g: g}
	c := NewCtxPtr(spell, 0, CtxInit{})
	// Every read goes through the head DISPATCH (EvalCountOK), so the test
	// fails if the head's arm is unregistered or reverted to a live scan.
	count := func(spec string) (int32, bool) {
		return EvalCountOK(h, c, "Count$LastStateBattlefieldWithFallback "+spec)
	}

	// Precondition: with no snapshot the live battlefield fallback counts 3.
	if n, ok := count("Creature"); !ok || n != 3 {
		t.Fatalf("precondition: absent snapshot live fallback = %d (ok %v), want 3", n, ok)
	}

	// Freeze only the FIRST Bear, with a captured power of 7: the frozen read
	// must be 1 (not 3), and $GreatestCardPower must be the captured 7, not
	// the live face's 1.
	snap := state.FreezeBattlefield(g, ids[:1], []state.FrozenPT{{ID: ids[0], Power: 7, Toughness: 2}})
	g.Obj(spell).CastBattlefield = snap
	if n, ok := count("Creature"); !ok || n != 1 {
		t.Fatalf("frozen battlefield = %d (ok %v), want the frozen 1", n, ok)
	}
	if n, ok := count("Creature$GreatestCardPower"); !ok || n != 7 {
		t.Fatalf("frozen greatest power = %d (ok %v), want the captured 7 (live face is 1)", n, ok)
	}

	// Present-but-empty is the authoritative zero, not the fallback.
	g.Obj(spell).CastBattlefield = state.FreezeBattlefield(g, nil, nil)
	if n, ok := count("Creature"); !ok || n != 0 {
		t.Fatalf("present-empty frozen battlefield = %d (ok %v), want the authoritative 0", n, ok)
	}

	// An empty filter is not a body this build can count.
	if _, ok := EvalCountOK(h, c, "Count$LastStateBattlefieldWithFallback"); ok {
		t.Fatal("empty argument must fail the head's own verdict")
	}
}
