package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// These tests pin the resolution-start stack-kind snapshot the SpellTargeted
// count ref reads (task agent-20260920T120233Z-357a4f24, round t2).
//
// The l1 round made SpellTargeted require the named target to be a live stack
// object, which is right for a battlefield permanent but WRONG once a move
// happens earlier in the same resolution: Reject Imperfection counters the
// spell BEFORE its DBProliferate sub reads X, Gale's Redirection exiles it
// before DBRoll reads Y, and Press the Enemy returns it before DBMayPlay reads
// X/Z. In every case the spell's mana VALUE is still readable (a face's
// converted cost is printed and the object id is stable), but the live ZStack
// test answered "not the spell" and returned 0. The fix captures which object
// targets were spells at effects.Resolve entry (Ctx.TargetSpellLKI) and reads
// that snapshot afterwards.
//
// Each test drives a REAL Resolve chain, so the move is executed by the real
// Counter/ChangeZone primitive through the events fold before the SVar is
// read, exactly the reviewer's probe (`MoveZone` for a stack spell).

// probeSpellMV is the SA body the registered effect records the evaluated
// SpellTargeted$CardManaCostLKI body into. A registered test effect keeps the
// assertion at the exact point a real chained sub-ability would read it.
var probeSpellMV int32
var probeSpellMVOK bool

// registerSpellMVProbe installs the recording effect for one test and removes
// it on cleanup (the context_test.go TestA/TestB/TestC convention), so the
// process-global registry is never left with a test-only entry.
func registerSpellMVProbe(t *testing.T) {
	t.Helper()
	Register("TestReadSpellMV", func(h Host, c *Ctx, s *cards.SA) {
		probeSpellMV, probeSpellMVOK = EvalCountOK(h, c, "SpellTargeted$CardManaCostLKI")
	})
	t.Cleanup(func() { unregister("TestReadSpellMV") })
}

// spellMVProbe is a synthetic instant whose body moves its target off the
// stack with the given move API and then reads SpellTargeted$CardManaCostLKI
// in a chained sub-ability -- the exact shape Reject Imperfection (Counter)
// and Gale's Redirection (ChangeZone exile) share.
func spellMVProbe(t *testing.T, move string) *cards.SA {
	t.Helper()
	line := move + " | Defined$ Targeted | SubAbility$ DBRead\n" +
		"SVar:DBRead:DB$ TestReadSpellMV\n"
	src := "Name:Spell MV Probe\nTypes:Instant\nA:" + line + "Oracle:x\n"
	c, d := cards.ParseBytes("probe.txt", []byte(src))
	if len(d) != 0 {
		t.Fatalf("probe diags: %v", d)
	}
	c.Link()
	return c.Faces[0].Abilities[0]
}

// stackSpell is the targeted spell every case in this file counters/exiles:
// mana value 4, on the stack, controlled by seat 1. Its face value differs
// from the probe source's (0), so a wrong-object read cannot coincide.
func targetedStackSpell(t *testing.T, h *fakeHost) state.ObjID {
	t.Helper()
	spell := mkCard(t, "Name:Targeted Spell\nManaCost:3 R\nTypes:Instant\nOracle:x\n")
	obj := h.g.AddObject(spell, 1)
	sp := h.g.Obj(obj.ID)
	sp.Zone = state.ZStack
	h.g.SetZone(state.ZStack, 1, []state.ObjID{obj.ID})
	return obj.ID
}

// TestSpellTargetedReadsManaValueAfterCounter is the reviewer's exact break:
// Reject Imperfection's SVar X is SpellTargeted$CardManaCostLKI and its
// DBProliferate gate reads it AFTER the root Counter has already moved the
// spell to the graveyard. The value must still be the spell's mana value 4,
// not the 0 the live-zone test returned.
func TestSpellTargetedReadsManaValueAfterCounter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Reject Imperfection")
	if !ok {
		t.Fatal("corpus missing Reject Imperfection")
	}
	if body := card.Faces[0].SVars["X"]; body != "SpellTargeted$CardManaCostLKI" {
		t.Fatalf("Reject Imperfection SVar X = %q, want SpellTargeted$CardManaCostLKI", body)
	}

	registerSpellMVProbe(t)
	h, c := fixtureHost(t)
	spellID := targetedStackSpell(t, h)
	c.Targets = []state.Target{{Obj: spellID}}
	// Precondition: the target really is a cmc-4 stack object and the probe
	// source's own mana value differs, so the read is unambiguous.
	if o := h.g.Obj(spellID); o == nil || o.Zone != state.ZStack || o.Face() == nil || o.Face().Cmc() != 4 {
		t.Fatalf("precondition: target = %+v, want a cmc-4 stack spell", h.g.Obj(spellID))
	}

	probeSpellMV, probeSpellMVOK = -1, false
	Resolve(h, c, spellMVProbe(t, "SP$ Counter"))
	// Postcondition the rule reads: the Counter really moved the spell off the
	// stack before the chained read.
	if o := h.g.Obj(spellID); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("postcondition: target = %+v, want moved to the graveyard before the SVar read", h.g.Obj(spellID))
	}
	if o := h.g.Obj(spellID); o != nil && o.Face() != nil && o.Face().Cmc() != 4 {
		t.Fatalf("postcondition: moved target mana value = %d, want 4 (the face value survives the move)", o.Face().Cmc())
	}
	if !probeSpellMVOK {
		t.Fatal("SpellTargeted$CardManaCostLKI was not evaluated; the ref failed closed")
	}
	if probeSpellMV != 4 {
		t.Errorf("SpellTargeted$CardManaCostLKI after Counter = %d, want 4 (the countered spell's mana value)", probeSpellMV)
	}
}

// TestSpellTargetedReadsManaValueAfterExile is the ChangeZone variant: Gale's
// Redirection exiles the targeted spell (ManaCost 2 U U, mana value 4) and
// then its DBRoll reads Y:SpellTargeted$CardManaCostLKI as the roll modifier.
// The exiled card's face value must survive the move.
func TestSpellTargetedReadsManaValueAfterExile(t *testing.T) {
	registerSpellMVProbe(t)
	h, c := fixtureHost(t)
	spell := mkCard(t, "Name:Exiled Spell\nManaCost:2 U U\nTypes:Instant\nOracle:x\n")
	obj := h.g.AddObject(spell, 1)
	sp := h.g.Obj(obj.ID)
	sp.Zone = state.ZStack
	h.g.SetZone(state.ZStack, 1, []state.ObjID{obj.ID})
	c.Targets = []state.Target{{Obj: obj.ID}}
	if o := h.g.Obj(obj.ID); o == nil || o.Zone != state.ZStack || o.Face() == nil || o.Face().Cmc() != 4 {
		t.Fatalf("precondition: target = %+v, want a cmc-4 stack spell", h.g.Obj(obj.ID))
	}

	probeSpellMV, probeSpellMVOK = -1, false
	Resolve(h, c, spellMVProbe(t, "DB$ ChangeZone | Origin$ Stack | Destination$ Exile"))
	if o := h.g.Obj(obj.ID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("postcondition: target = %+v, want exiled before the SVar read", h.g.Obj(obj.ID))
	}
	if !probeSpellMVOK || probeSpellMV != 4 {
		t.Errorf("SpellTargeted$CardManaCostLKI after exile = (%d,%v), want (4,true)", probeSpellMV, probeSpellMVOK)
	}
}

// TestSpellTargetedExcludesPermanentTargetAfterMoveThroughResolve proves the
// capture does NOT re-admit the l1 regression's permanent: Press the Enemy's
// target is a battlefield permanent (mana value 4), so the resolution-start
// snapshot records no spell and SpellTargeted stays a legitimate zero while
// the sibling Targeted body reads 4 -- even when the chain moves the permanent
// (the return-to-hand path) before a chained read.
func TestSpellTargetedExcludesPermanentTargetAfterMoveThroughResolve(t *testing.T) {
	registerSpellMVProbe(t)
	h, c := fixtureHost(t)
	permanent := mkCard(t, "Name:Permanent Target\nManaCost:4\nTypes:Artifact\nOracle:x\n")
	obj := h.g.AddObject(permanent, 1)
	h.g.Obj(obj.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 1, []state.ObjID{obj.ID})
	c.Targets = []state.Target{{Obj: obj.ID}}
	if o := h.g.Obj(obj.ID); o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || o.Face().Cmc() != 4 {
		t.Fatalf("precondition: target = %+v, want a cmc-4 battlefield permanent", h.g.Obj(obj.ID))
	}

	probeSpellMV, probeSpellMVOK = -1, false
	// The move (battlefield -> hand) runs before the chained read, so a
	// live-zone fallback would now see a non-stack object; the snapshot, not
	// the zone, must be what answers.
	Resolve(h, c, spellMVProbe(t, "DB$ ChangeZone | Origin$ Battlefield | Destination$ Hand"))
	if o := h.g.Obj(obj.ID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("postcondition: target = %+v, want in hand before the SVar read", h.g.Obj(obj.ID))
	}
	if !probeSpellMVOK || probeSpellMV != 0 {
		t.Errorf("SpellTargeted$CardManaCostLKI on a moved permanent = (%d,%v), want (0,true)", probeSpellMV, probeSpellMVOK)
	}
	// The sibling ref still reads the permanent's mana value, proving the
	// zero comes from the ref's kind filter, not from a missing target.
	if got := EvalCount(h, c, "Targeted$CardManaCostLKI"); got != 4 {
		t.Errorf("Targeted$CardManaCostLKI on the moved permanent = %d, want 4", got)
	}
}

// TestSpellTargetedLiveZoneFallbackWithoutSnapshot pins the fallback the
// hand-built Ctx path keeps: with no Resolve chain there is no snapshot, so a
// directly-evaluated body reads a still-on-stack spell (the existing
// count_ref_vocab_test.go path) and returns zero once the object has left the
// stack -- the behaviour the resolution-start snapshot exists to fix.
func TestSpellTargetedLiveZoneFallbackWithoutSnapshot(t *testing.T) {
	h, c := fixtureHost(t)
	spell := mkCard(t, "Name:Direct Spell\nManaCost:2 R\nTypes:Sorcery\nOracle:x\n")
	obj := h.g.AddObject(spell, 1)
	h.g.Obj(obj.ID).Zone = state.ZStack
	h.g.SetZone(state.ZStack, 1, []state.ObjID{obj.ID})
	c.Targets = []state.Target{{Obj: obj.ID}}
	if c.Snap.TargetSpell != nil {
		t.Fatal("precondition: a fresh Ctx must not carry a snapshot")
	}
	if got := EvalCount(h, c, "SpellTargeted$CardManaCostLKI"); got != 3 {
		t.Errorf("live-stack fallback = %d, want 3", got)
	}
	h.g.Obj(obj.ID).Zone = state.ZGraveyard
	h.g.SetZone(state.ZStack, 1, nil)
	if got := EvalCount(h, c, "SpellTargeted$CardManaCostLKI"); got != 0 {
		t.Errorf("moved target with no snapshot = %d, want 0 (the fallback cannot know the kind)", got)
	}
}
