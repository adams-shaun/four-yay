package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// Task putcounter-anyzone (CR 122.1): counters can exist on an object in ANY
// zone, not just the battlefield -- a suspended card's time counters sit on
// its exiled card (CR 702.62a), and The Tenth Doctor's recalled permanent
// takes them in exile. effPutCounter's ordinary target loop carried a
// battlefield-only precondition that silently dropped the whole instruction
// for a non-battlefield recipient (ETB$ True was the sole tolerated
// exception); these pins resolve the REAL effPutCounter through Resolve with
// an inline SA whose Defined$ referent is a remembered object in exile, and
// assert both the zone precondition and the counter delta.

// putCounterExiledObject adds one card to seat 0's exile zone and returns it.
func putCounterExiledObject(t *testing.T, h *fakeHost) *state.Object {
	t.Helper()
	card := mkCard(t, "Name:ExiledTee\nTypes:Sorcery\nOracle:x\n")
	o := h.g.AddObject(card, 0)
	o.Zone = state.ZExile
	return o
}

// TestPutCounterPlacesOnExiledObject is the defect pin: a PutCounter whose
// Defined$ referent is a remembered card in EXILE must place the requested
// counters on it. On the unmodified tree the ordinary loop's
// `o.Zone != state.ZBattlefield` gate skips the exiled target, so no
// CounterChange reaches it and its counter count stays 0 -- this fails.
func TestPutCounterPlacesOnExiledObject(t *testing.T) {
	h := newHost(t, 2)
	exiled := putCounterExiledObject(t, h)

	// Preconditions the real assertion depends on: the object really is in
	// exile, and it really has no TIME counters before resolution (so a
	// pass cannot come from a pre-seeded count).
	if exiled.Zone != state.ZExile {
		t.Fatalf("precondition: target zone = %s, want Exile", exiled.Zone)
	}
	if got := exiled.Counter("TIME"); got != 0 {
		t.Fatalf("precondition: target already has %d TIME counters, want 0", got)
	}

	c := &Ctx{Source: exiled.ID, Controller: 0,
		Remembered: []state.Target{{Obj: exiled.ID}}}
	Resolve(h, c, sa(t, "SP$ PutCounter | Defined$ Remembered | CounterType$ TIME | CounterNum$ 3"))

	if got := exiled.Counter("TIME"); got != 3 {
		t.Fatalf("exiled object TIME counters = %d, want 3", got)
	}
	if exiled.Zone != state.ZExile {
		t.Fatalf("target left exile: zone = %s", exiled.Zone)
	}
	// The mutation must arrive as a real CounterChange event, not a direct
	// write (the state assert above reads the fold).
	if n := counterChangeCount(h, exiled.ID); n != 1 {
		t.Fatalf("CounterChange events for the exiled object = %d, want 1", n)
	}
}

// TestPutCounterPlacesOnBattlefieldObject is the no-regression companion: the
// ordinary battlefield recipient must still take its counters. It is a guard
// (green on both the unmodified and modified tree) that pins the widened gate
// did not disturb the common case.
func TestPutCounterPlacesOnBattlefieldObject(t *testing.T) {
	h := newHost(t, 2)
	permanent := putCounterObject(t, h)
	obj := h.g.Obj(permanent)

	if obj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: target zone = %s, want Battlefield", obj.Zone)
	}
	if got := obj.Counter("TIME"); got != 0 {
		t.Fatalf("precondition: target already has %d TIME counters, want 0", got)
	}

	c := &Ctx{Source: permanent, Controller: 0,
		Remembered: []state.Target{{Obj: permanent}}}
	Resolve(h, c, sa(t, "SP$ PutCounter | Defined$ Remembered | CounterType$ TIME | CounterNum$ 3"))

	if got := obj.Counter("TIME"); got != 3 {
		t.Fatalf("battlefield object TIME counters = %d, want 3", got)
	}
	if n := counterChangeCount(h, permanent); n != 1 {
		t.Fatalf("CounterChange events for the battlefield object = %d, want 1", n)
	}
}
