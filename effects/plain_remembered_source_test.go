package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPlainRememberedExplicitSource pins the plain-Remembered counterpart of
// TestRememberedLKIGroupKeepsExplicitlyRememberedSource (task
// agent-20260925T094320Z-60e2024d): the plain group and the RememberedLKI
// group are ONE resolution rule, so an object that IS the resolving source and
// was EXPLICITLY remembered after the trigger's fire-time capture must be
// visible to every plain-Remembered reader.
//
// The real card is Lukamina, Moon Druid. Specialized into Crocodile Form, it
// dies and its per-face trigger's `SVar:TrigUnspecialize` runs
// `DB$ SetState | Defined$ TriggeredCard | Mode$ Unspecialize |
// RememberChanged$ True | SubAbility$ DBReturn`, whose chained
// `SVar:DBReturn:DB$ ChangeZone | ConditionDefined$ Remembered |
// ConditionPresent$ Card | Origin$ Graveyard | Destination$ Battlefield |
// Tapped$ True | Defined$ Remembered` returns it tapped. setstateRememberChanged
// (effects/misc.go) appends that same object to Ctx.Remembered; the object IS
// Ctx.Source (the source changed its own state). An earlier build's blanket
// `t.Obj != c.Source` guard in rememberedWithSource dropped that real memory,
// so the gate read zero and ChangeZone moved nothing.
//
// The capture-occurrence exclusion (rememberedExcludingCapture) already
// removes exactly the seeded fire-time capture, so the blanket source drop was
// always redundant for its stated purpose and only ever deleted a real memory.
func TestPlainRememberedExplicitSource(t *testing.T) {
	h := newHost(t, 2)
	// The source is a creature with a distinct, non-default toughness, so a
	// property read cannot pass by coincidence.
	src := mkCard(t, "Name:Lukamina\nTypes:Creature Druid\nPT:2/5\nOracle:x\n")
	so := h.g.AddObject(src, 0)
	h.g.Obj(so.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), so.ID))

	// Precondition: the source is really on the battlefield and its printed
	// toughness really differs from the zero a dropped memory would read.
	if o := h.g.Obj(so.ID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source not on battlefield: %+v", o)
	}
	toughness := int32(h.g.Obj(so.ID).Face().Toughness())
	if toughness != 5 {
		t.Fatalf("precondition: source printed toughness = %d, want 5", toughness)
	}

	// --- capture-only ctx (the trigger's fire-time capture is the source and
	// the resolution remembered nothing): the source stays excluded. This is
	// the contrast that proves the fix admits EXPLICIT memory, not every
	// source occurrence.
	captureOnly := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if len(captureOnly.Remembered) != 1 || captureOnly.Remembered[0] != captureOnly.Captured[0] {
		t.Fatalf("precondition: capture-only ctx not Remembered==Captured==source")
	}
	presence := sa(t, "DB$ Pump | ConditionDefined$ Remembered | ConditionPresent$ Card")
	if met, resolved := conditionMet(h, captureOnly, presence); !resolved || met {
		t.Fatalf("capture-only ConditionPresent$ Card: met=%v resolved=%v, want false true (capture must not satisfy the gate)", met, resolved)
	}
	if got := EvalCount(h, captureOnly, "Remembered$CardToughness"); got != 0 {
		t.Fatalf("capture-only Remembered$CardToughness = %d, want 0", got)
	}

	// --- captured-then-explicitly-remembered ctx (the Lukamina chain): the
	// same id appears twice, once as the seeded capture and once as the
	// explicit SetState/RememberChanged append. The exclusion removes only the
	// seeded occurrence, so the explicit one survives.
	explicitCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}, {Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if met, resolved := conditionMet(h, explicitCtx, presence); !resolved || !met {
		t.Fatalf("explicit-source ConditionPresent$ Card: met=%v resolved=%v, want true true (explicitly-remembered source must satisfy the gate)", met, resolved)
	}
	if got := EvalCount(h, explicitCtx, "Remembered$CardToughness"); got != toughness {
		t.Fatalf("explicit-source Remembered$CardToughness = %d, want %d", got, toughness)
	}
	// The group contains exactly one object (the kept explicit source), not
	// the capture occurrence as well.
	if got := EvalCount(h, explicitCtx, "Remembered$Amount"); got != 1 {
		t.Fatalf("explicit-source Remembered$Amount = %d, want 1", got)
	}

	// --- persistent source memory intact: a source whose card-level
	// Remembered list holds a distinct object still surfaces it, and a
	// distinct-event capture is still filtered.
	beast := mkCard(t, "Name:Beast\nTypes:Creature Beast\nPT:3/4\nOracle:x\n")
	bo := h.g.AddObject(beast, 0)
	h.g.Obj(bo.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), bo.ID))
	// Wait: bo was minted after so; append it to the source's persistent list.
	h.g.Obj(so.ID).Remembered = []state.Target{{Obj: bo.ID}}
	persistentCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: so.ID}},
		Captured:   []state.Target{{Obj: so.ID}},
	}
	if o := h.g.Obj(bo.ID); o == nil || o.Face().Toughness() != 4 {
		t.Fatalf("precondition: persistent member not a creature with toughness 4")
	}
	if met, resolved := conditionMet(h, persistentCtx, presence); !resolved || !met {
		t.Fatalf("persistent-memory ConditionPresent$ Card: met=%v resolved=%v, want true true", met, resolved)
	}
	if got := EvalCount(h, persistentCtx, "Remembered$CardToughness"); got != 4 {
		t.Fatalf("persistent-memory Remembered$CardToughness = %d, want 4 (beast only)", got)
	}

	// --- distinct-event capture still excluded: capture is an object that is
	// NOT the source, and the source has no persistent memory. The capture is
	// all the ctx has and must read empty.
	h.g.Obj(so.ID).Remembered = nil
	eventCtx := &Ctx{
		Source:     so.ID,
		Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}},
		Captured:   []state.Target{{Obj: bo.ID}},
	}
	if bo.ID == so.ID {
		t.Fatalf("precondition: event capture coincides with the source")
	}
	if met, resolved := conditionMet(h, eventCtx, presence); !resolved || met {
		t.Fatalf("distinct-capture ConditionPresent$ Card: met=%v resolved=%v, want false true", met, resolved)
	}
}
