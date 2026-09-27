package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestDefinedRememberedLKICaptureExcluded pins the capture exclusion the
// `Defined$ RememberedLKI` spelling now applies (and the matching
// `ConditionDefined$ RememberedLKI` gate), the same contract the count path's
// RememberedLKI ref group already honours (effects/count.go
// rememberedLKIGroup).
//
// rules seeds a firing trigger's execution ctx with Remembered == Captured ==
// the trigger's fire-time event capture. Forge's remembered list never
// contains the event object the trigger fired on -- the trigger body reads it
// through the separate Triggered* family -- so a raw objectsOf(c.Remembered)
// read treats the capture as something the resolution itself remembered. The
// measured symptom: Nurturing Pixie's ETB returned no permanent, yet the
// trigger's own self-capture (the Pixie, a permanent) made
// `ConditionDefined$ RememberedLKI | ConditionPresent$ Card.Permanent` true
// and granted the +1/+1 counter.
//
// Each sub-case asserts its own precondition -- the capture object really has
// the property the gate reads (a Permanent), the source and event object are
// really distinct where that matters, and the explicit remember really sits in
// the ctx list -- so a vacuous fixture fails loudly instead of passing on an
// empty list.
func TestDefinedRememberedLKICaptureExcluded(t *testing.T) {
	h := newHost(t, 2)
	// A creature, so `Card.Permanent` matches it on the battlefield and the
	// gate would fire if the capture leaked through.
	beast := mkCard(t, "Name:Beast\nManaCost:3\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
	// The trigger's source: a creature too -- the phase shape where the
	// capture happens to BE the source.
	src := mkCard(t, "Name:Src\nManaCost:5\nTypes:Creature\nPT:2/2\nOracle:x\n")
	// A non-creature permanent, for the no-capture positive branch.
	rock := mkCard(t, "Name:Rock\nManaCost:7\nTypes:Artifact\nOracle:x\n")
	bo := h.g.AddObject(beast, 0)
	so := h.g.AddObject(src, 0)
	ro := h.g.AddObject(rock, 0)
	for _, id := range []state.ObjID{bo.ID, so.ID, ro.ID} {
		h.g.Obj(id).Zone = state.ZBattlefield
		h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), id))
	}
	// A real permanent is required for `Card.Permanent` to be a live
	// question; a vacuous board would deny for the wrong reason.
	if o := h.g.Obj(so.ID); o == nil || o.Zone != state.ZBattlefield || !o.Face().IsCreature() {
		t.Fatalf("precondition: source is not a battlefield creature/permanent")
	}
	if o := h.g.Obj(ro.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: rock is not on the battlefield")
	}

	// The corpus gate exactly as Nurturing Pixie spells it.
	gate := sa(t, "DB$ PutCounter | ConditionDefined$ RememberedLKI | ConditionPresent$ Card.Permanent")

	// --- phase-trigger shape: capture == source, no real memory. Both the
	// direct Defined$ read and the condition gate must name nothing.
	phaseCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if len(phaseCtx.Remembered) != 1 || phaseCtx.Remembered[0] != phaseCtx.Captured[0] {
		t.Fatalf("precondition: phase ctx not seeded Remembered==Captured==source")
	}
	ts, ok := definedSpec(h, phaseCtx, "RememberedLKI")
	if !ok {
		t.Fatalf("Defined$ RememberedLKI unresolved, want a known empty set")
	}
	if len(ts) != 0 {
		t.Fatalf("Defined$ RememberedLKI over capture-only ctx = %+v, want empty (capture must not be memory)", ts)
	}
	if met, resolved := conditionMet(h, phaseCtx, gate); !resolved || met {
		t.Fatalf("ConditionDefined$ RememberedLKI|Present Card.Permanent over capture-only ctx: met=%v resolved=%v, want false true", met, resolved)
	}

	// --- event-object shape: capture = the event object, distinct from the
	// source; the remembered set holds only the capture.
	eventCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}},
		Captured:   []state.Target{{Obj: bo.ID}},
	}
	if bo.ID == so.ID {
		t.Fatalf("precondition: event capture coincides with the source")
	}
	if ts, ok := definedSpec(h, eventCtx, "RememberedLKI"); !ok || len(ts) != 0 {
		t.Fatalf("Defined$ RememberedLKI over event-capture ctx = %+v ok=%v, want empty true", ts, ok)
	}
	if met, resolved := conditionMet(h, eventCtx, gate); !resolved || met {
		t.Fatalf("event-capture gate: met=%v resolved=%v, want false true", met, resolved)
	}

	// --- no-capture ctx unchanged: a plain resolution's real member is named.
	spellCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: ro.ID}},
	}
	if ts, ok := definedSpec(h, spellCtx, "RememberedLKI"); !ok || len(ts) != 1 || ts[0].Obj != ro.ID {
		t.Fatalf("Defined$ RememberedLKI over no-capture ctx = %+v ok=%v, want [rock] true", ts, ok)
	}
	if met, resolved := conditionMet(h, spellCtx, gate); !resolved || !met {
		t.Fatalf("no-capture gate over a real permanent: met=%v resolved=%v, want true true", met, resolved)
	}

	// --- explicit later remember equal to the capture: the seeded occurrence
	// is dropped but the resolution's OWN remember of the same object stays
	// (the count path's rememberedExcludingCapture contract). The gate must
	// fire here because the object really was remembered.
	equalCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}, {Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if len(equalCtx.Remembered) != 2 {
		t.Fatalf("precondition: ctx does not hold the capture PLUS an explicit remember")
	}
	if ts, ok := definedSpec(h, equalCtx, "RememberedLKI"); !ok || len(ts) != 1 || ts[0].Obj != so.ID {
		t.Fatalf("Defined$ RememberedLKI with capture+explicit remember = %+v ok=%v, want [source] true", ts, ok)
	}
	if met, resolved := conditionMet(h, equalCtx, gate); !resolved || !met {
		t.Fatalf("explicit-remember gate: met=%v resolved=%v, want true true", met, resolved)
	}

	// --- source persistent memory is retained even when the capture is the
	// source (RememberLKI$ True on the source itself, Cosima's self-return).
	h.g.Obj(so.ID).Remembered = []state.Target{{Obj: ro.ID}}
	if ts, ok := definedSpec(h, phaseCtx, "RememberedLKI"); !ok || len(ts) != 1 || ts[0].Obj != ro.ID {
		t.Fatalf("Defined$ RememberedLKI with source persistent memory = %+v ok=%v, want [rock] true", ts, ok)
	}

	// --- Triggered* resolution is untouched: the capture is still exactly
	// what Defined$ TriggeredCard names.
	if ts, ok := definedSpec(h, eventCtx, "TriggeredCard"); !ok || len(ts) != 1 || ts[0].Obj != bo.ID {
		t.Fatalf("Defined$ TriggeredCard over event capture = %+v ok=%v, want [beast] true (Triggered* must be untouched)", ts, ok)
	}

	// --- player distinction retained: a remembered player is named by the
	// LKI resolver (and then filtered out of the object-only Defined$ read).
	// Clear the source's persistent list set by the previous sub-case so the
	// read names only the player.
	h.g.Obj(so.ID).Remembered = nil
	playerCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Player: 1, IsPlayer: true}},
	}
	if ts, ok := definedSpec(h, playerCtx, "RememberedLKI"); !ok || len(ts) != 0 {
		t.Fatalf("Defined$ RememberedLKI over a remembered player = %+v ok=%v, want empty true (object-only)", ts, ok)
	}
	if grp := rememberedLKIGroup(h, playerCtx); len(grp) != 1 || !grp[0].IsPlayer {
		t.Fatalf("rememberedLKIGroup over a remembered player = %+v, want the player retained", grp)
	}
}
