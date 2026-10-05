package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// EOE Unravel — `1 U U` Instant — "Counter target spell. If the amount of
// mana spent to cast that spell was less than its mana value, you draw a
// card."
//
// The chained DBDraw condition reads SVar:X:Targeted$CastTotalManaSpent AFTER
// the Counter has moved the target spell off the stack. events.Apply zeroes
// the cast-time captures on that move (CR 400.7), so the live read is a
// legitimate-looking 0 and the gate wrongly fires. This file pins the
// resolution-start snapshot that answers the read instead.

// TestTargetedCastTotalManaSpentReadsTheSnapshot pins the two halves of the
// Targeted$ form: a target still on the stack reads its LIVE captures, and a
// target that has left the stack reads the snapshot Resolve captured at
// entry. The precondition asserts the live field really is cleared, so the
// snapshot assertion cannot pass by reading the live object.
func TestTargetedCastTotalManaSpentReadsTheSnapshot(t *testing.T) {
	h, c := fixtureHost(t)
	spell := h.g.AddObject(mkCard(t, "Name:Targeted Spell\nManaCost:3 R\nTypes:Sorcery\nOracle:x\n"), 1)

	// On the stack: the live read is authoritative and returns the real 7.
	spell.Zone = state.ZStack
	spell.ManaSpent = 7
	spell.ManaSnowSpent = 2
	c.Targets = []state.Target{{Obj: spell.ID}}
	if got := EvalCount(h, c, "Targeted$CastTotalManaSpent"); got != 7 {
		t.Fatalf("on-stack Targeted$CastTotalManaSpent = %d, want 7 (live read)", got)
	}
	if got := EvalCount(h, c, "Targeted$CastTotalManaSpent Snow"); got != 2 {
		t.Fatalf("on-stack Targeted$CastTotalManaSpent Snow = %d, want 2 (live read)", got)
	}

	// The chain's resolution-start snapshot captured the spend.
	c.Snap.TargetManaSpent = map[state.ObjID]castManaSpentTotals{
		spell.ID: {total: 7, snow: 2},
	}

	// Simulate the Counter: the spell leaves the stack, which zeroes the live
	// captures (events.Apply's wasStack sweep). Precondition: the live fields
	// really are cleared, so snapshot and live genuinely differ.
	h.Emit(events.Event{Kind: events.MoveZone, Obj: spell.ID, From: state.ZStack, To: state.ZGraveyard})
	if o := h.g.Obj(spell.ID); o == nil || o.Zone != state.ZGraveyard || o.ManaSpent != 0 {
		t.Fatalf("precondition: countered spell = %+v, want off-stack with live ManaSpent 0", h.g.Obj(spell.ID))
	}

	if got := EvalCount(h, c, "Targeted$CastTotalManaSpent"); got != 7 {
		t.Errorf("off-stack Targeted$CastTotalManaSpent = %d, want 7 (the resolution-start snapshot)", got)
	}
	if got := EvalCount(h, c, "Targeted$CastTotalManaSpent Snow"); got != 2 {
		t.Errorf("off-stack Targeted$CastTotalManaSpent Snow = %d, want 2 (snapshot)", got)
	}
	// A target absent from the snapshot still falls back to the (zeroed) live
	// read rather than inventing a value.
	c.Targets = []state.Target{{Obj: 999}}
	if got := EvalCount(h, c, "Targeted$CastTotalManaSpent"); got != 0 {
		t.Errorf("uncaptured target Targeted$CastTotalManaSpent = %d, want 0", got)
	}
}

// TestTargetedCastTotalManaSpentCapturedAtResolveEntry proves the snapshot is
// really taken at effects.Resolve entry, not only when a test hands one in:
// a registered effect that emits the stack->graveyard move and then reads the
// targeted spend must see the pre-move value.
func TestTargetedCastTotalManaSpentCapturedAtResolveEntry(t *testing.T) {
	var readOnStack, readOffStack int32
	Register("TestCaptureSpend", func(h Host, c *Ctx, s *cards.SA) {
		readOnStack = EvalCount(h, c, "Targeted$CastTotalManaSpent")
		// Emit the counter's move; the live captures zero.
		h.Emit(events.Event{Kind: events.MoveZone, Obj: c.Targets[0].Obj, From: state.ZStack, To: state.ZGraveyard})
		readOffStack = EvalCount(h, c, "Targeted$CastTotalManaSpent")
	})
	t.Cleanup(func() { unregister("TestCaptureSpend") })

	h := newHost(t, 2)
	spell := h.g.AddObject(mkCard(t, "Name:Targeted Spell\nManaCost:3 R\nTypes:Sorcery\nOracle:x\n"), 1)
	spell.Zone = state.ZStack
	spell.ManaSpent = 7
	c := &Ctx{Source: spell.ID, Controller: 0, Targets: []state.Target{{Obj: spell.ID}}}

	// Precondition: the snapshot does NOT yet exist, so it can only come from
	// Resolve's own capture.
	if c.Snap.TargetManaSpent != nil {
		t.Fatal("precondition: snapshot already populated before Resolve")
	}
	body := sa(t, "DB$ TestCaptureSpend")
	Resolve(h, c, body)

	if readOnStack != 7 {
		t.Errorf("read on the stack = %d, want 7", readOnStack)
	}
	if o := h.g.Obj(spell.ID); o == nil || o.Zone != state.ZGraveyard || o.ManaSpent != 0 {
		t.Fatalf("precondition: after the move the live spend = %+v, want zone graveyard ManaSpent 0", h.g.Obj(spell.ID))
	}
	if readOffStack != 7 {
		t.Errorf("read after the move = %d, want 7 (the Resolve-entry snapshot)", readOffStack)
	}
}

// TestTargetedCastTotalManaSpentReadsTheRealCorpusSVar proves the real
// compiled Unravel script reaches the head: its SVar:X is exactly this
// spelling, and the same evaluation the chained DBDraw makes returns the real
// pre-move total instead of the pre-fix zero.
func TestTargetedCastTotalManaSpentReadsTheRealCorpusSVar(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Unravel")
	if !ok {
		t.Fatal("corpus missing Unravel")
	}
	body := card.Faces[0].SVars["X"]
	if body != "Targeted$CastTotalManaSpent" {
		t.Fatalf("Unravel SVar X = %q, want the target-relative total form", body)
	}
	h, c := fixtureHost(t)
	spell := h.g.AddObject(mkCard(t, "Name:Counterspell Target\nManaCost:2 U\nTypes:Instant\nOracle:x\n"), 1)
	c.Targets = []state.Target{{Obj: spell.ID}}
	c.Snap.TargetManaSpent = map[state.ObjID]castManaSpentTotals{spell.ID: {total: 5}}
	if got := EvalCount(h, c, body); got != 5 {
		t.Errorf("real Unravel SVar X = %d, want 5", got)
	}
}
