package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRememberedNumberCaptureExcluded pins the capture exclusion on
// Count$RememberedNumber (task rememberedcapture1, the consolidated acceptance
// clause). It is the same defect class as Remembered$Amount: rules seeds a
// firing trigger's ctx with Remembered == Captured == its fire-time event
// capture, and a raw len(Ctx.Remembered) counts that capture as something the
// resolution remembered, so a trigger body gated on "remembered nothing" can
// never hold.
//
// Each sub-case asserts its own precondition (the source is on the
// battlefield, the capture really is the source or really is distinct, and the
// precedence fields are really set) so a vacuous fixture fails loudly.
func TestRememberedNumberCaptureExcluded(t *testing.T) {
	h := newHost(t, 2)
	beast := mkCard(t, "Name:Beast\nManaCost:3\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
	src := mkCard(t, "Name:Src\nManaCost:5\nTypes:Creature\nPT:2/2\nOracle:x\n")
	bo := h.g.AddObject(beast, 0)
	so := h.g.AddObject(src, 0)
	for _, id := range []state.ObjID{bo.ID, so.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
		h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), id))
	}
	if h.g.Obj(so.ID) == nil || h.g.Obj(so.ID).Zone != state.ZBattlefield {
		t.Fatalf("precondition: source not on the battlefield")
	}

	// Phase-trigger shape: capture == source, real remembered set empty.
	phaseCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if len(phaseCtx.Remembered) != 1 || phaseCtx.Remembered[0] != phaseCtx.Captured[0] {
		t.Fatalf("precondition: phase ctx not seeded Remembered==Captured==source")
	}
	if n, ok := EvalCountOK(h, phaseCtx, "Count$RememberedNumber"); !ok || n != 0 {
		t.Fatalf("phase-capture Count$RememberedNumber = %d ok=%v, want 0 true (capture must not count)", n, ok)
	}

	// Event-object shape: capture = the event object, distinct from the
	// source; only the capture is remembered.
	eventCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}},
		Captured:   []state.Target{{Obj: bo.ID}},
	}
	if bo.ID == so.ID {
		t.Fatalf("precondition: event capture coincides with the source")
	}
	if n, ok := EvalCountOK(h, eventCtx, "Count$RememberedNumber"); !ok || n != 0 {
		t.Fatalf("event-capture Count$RememberedNumber = %d ok=%v, want 0 true", n, ok)
	}

	// Capture PLUS a real remembered member: the count is the real memory.
	mixedCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}, {Obj: so.ID}},
		Captured:   []state.Target{{Obj: bo.ID}},
	}
	if n, ok := EvalCountOK(h, mixedCtx, "Count$RememberedNumber"); !ok || n != 1 {
		t.Fatalf("mixed Count$RememberedNumber = %d ok=%v, want 1 true (source only, capture excluded)", n, ok)
	}

	// No-capture ctx unchanged: a plain spell resolution counts every
	// remembered object.
	spellCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}, {Obj: so.ID}},
	}
	if n, ok := EvalCountOK(h, spellCtx, "Count$RememberedNumber"); !ok || n != 2 {
		t.Fatalf("no-capture Count$RememberedNumber = %d ok=%v, want 2 true (unchanged)", n, ok)
	}

	// FlipMemory precedence survives the exclusion: a DB$ FlipCoin
	// RememberNumber$ publication is the remembered number even when the ctx
	// holds a capture.
	flipCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
		FlipMemory: &FlipMemory{RememberNumber: 5, RememberNumberKind: "Heads"},
	}
	if n, ok := EvalCountOK(h, flipCtx, "Count$RememberedNumber"); !ok || n != 5 {
		t.Fatalf("FlipMemory Count$RememberedNumber = %d ok=%v, want 5 true (publication precedence)", n, ok)
	}

	// RememberedCMCBound precedence survives too: the countered spell's mana
	// value, not a count of remembered entries.
	cmcCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
		Num:        NumberInputs{RememberedCMC: 7, RememberedCMCBound: true},
	}
	if n, ok := EvalCountOK(h, cmcCtx, "Count$RememberedNumber"); !ok || n != 7 {
		t.Fatalf("RememberedCMCBound Count$RememberedNumber = %d ok=%v, want 7 true (CMC precedence)", n, ok)
	}
}
