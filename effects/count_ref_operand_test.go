package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRefPropertyOpResolvesNamedSVarOperand pins the <Ref>$<Property>/Op
// suffix's SVar-named operand (task d8b-permanents, Doran, Besieged by
// Time): "SVar:X1:SVar$Y1/Abs", "SVar:Y1:TriggeredAttacker$CardPower/Minus.Z1",
// "SVar:Z1:TriggeredAttacker$CardToughness" — the pump amount is
// |power - toughness|, read through a two-deep SVar chain whose /Minus
// operand is another SVar. Before the operand-aware read the numeric-only
// applyCountOp parsed no number in "Minus.Z1", left the base value standing
// and turned the difference into "+power/+power": a 0/5 Doran pumped +0/+0,
// a 2/2 blocker pumped +2/+2.
func TestRefPropertyOpResolvesNamedSVarOperand(t *testing.T) {
	h := newHost(t, 2)
	doran := mkCard(t, "Name:Doran\nManaCost:1\nTypes:Creature Treefolk\nPT:3/5\nOracle:x\n")
	bear := mkCard(t, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	do := h.g.AddObject(doran, 0)
	bo := h.g.AddObject(bear, 0)
	c := &Ctx{Source: do.ID, Controller: 0,
		Remembered: []state.Target{{Obj: do.ID}},
		SVars: map[string]string{
			"X1": "SVar$Y1/Abs",
			"Y1": "TriggeredAttacker$CardPower/Minus.Z1",
			"Z1": "TriggeredAttacker$CardToughness",
		}}
	// Precondition: the referenced creature really reads 0/5 through the
	// same properties the chain composes, so a wrong property arm (power
	// where toughness was asked) cannot satisfy the final answer.
	if p, ok := EvalCountOK(h, c, "TriggeredAttacker$CardPower"); !ok || p != 3 {
		t.Fatalf("precondition: CardPower = %d (ok %v), want 3", p, ok)
	}
	if tgh, ok := EvalCountOK(h, c, "TriggeredAttacker$CardToughness"); !ok || tgh != 5 {
		doO := h.Game().Obj(do.ID)
		t.Fatalf("precondition: CardToughness = %d (ok %v), want 5; face P/T read %v/%v zone %v",
			tgh, ok, doO.Face().Power(), doO.Face().Toughness(), doO.Zone)
	}
	if got := EvalCount(h, c, "SVar$X1"); got != 2 {
		t.Fatalf("|3-5| through SVar$Y1/Abs = %d, want 2", got)
	}
	// The Blocks twin reads the blocker: a 2/2's difference is 0, so the
	// pump grants +0/+0 and the bear stays 2/2.
	bc := &Ctx{Source: do.ID, Controller: 0,
		Remembered: []state.Target{{Obj: bo.ID}},
		SVars: map[string]string{
			"X2": "SVar$Y2/Abs",
			"Y2": "TriggeredBlocker$CardPower/Minus.Z2",
			"Z2": "TriggeredBlocker$CardToughness",
		}}
	if got := EvalCount(h, bc, "SVar$X2"); got != 0 {
		t.Fatalf("|2-2| through SVar$Y2/Abs = %d, want 0", got)
	}
	// The same operand grammar on a Remembered ref and on the
	// TargetedObjects Amount shortcut (the family's other op sites).
	rc := &Ctx{Source: do.ID, Controller: 0, Remembered: []state.Target{{Obj: bo.ID}, {Obj: bo.ID}},
		SVars: map[string]string{"W": "Remembered$Amount/Minus.Z", "Z": "Number$1"}}
	if got := EvalCount(h, rc, "SVar$W"); got != 1 {
		t.Fatalf("Remembered$Amount/Minus.Z (Z=1) = %d, want 1", got)
	}
}

// TestCountOpAbs pins the /Abs op suffix (Doran's SVar$Y1/Abs): the
// magnitude of the value. Combined with a negative Minus it turns
// power-minus-toughness into the difference the pump sizes.
func TestCountOpAbs(t *testing.T) {
	h := newHost(t, 2)
	doran := mkCard(t, "Name:Doran\nManaCost:1\nTypes:Creature Treefolk\nPT:3/5\nOracle:x\n")
	do := h.g.AddObject(doran, 0)
	c := &Ctx{Source: do.ID, Controller: 0, Remembered: []state.Target{{Obj: do.ID}},
		SVars: map[string]string{"W": "Number$0/Minus.5"}}
	// Precondition: the base chain reads NEGATIVE (the saturating clamp is
	// the int32 range, not zero), or an Abs that clamps would pass vacuously.
	if got := EvalCount(h, c, "SVar$W"); got != -5 {
		t.Fatalf("precondition: 0-5 = %d, want -5", got)
	}
	if got := EvalCount(h, c, "SVar$W/Abs"); got != 5 {
		t.Fatalf("Abs(-5) = %d, want 5", got)
	}
	if got := EvalCount(h, c, "Number$7/Abs"); got != 7 {
		t.Fatalf("Abs(7) = %d, want 7", got)
	}
}
