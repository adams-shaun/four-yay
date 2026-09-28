package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TargetedByTarget$Valid <spec> (Not of This World's SVar:CheckTgt and Bane's
// Contingency's, the corpus's two carriers): the count of objects among the
// targets of the spell(s) the resolving source targets that match <spec> --
// the NESTED read one level past the Targeted ref. These tests build the
// shape directly: a battlefield creature, a stack spell targeting it, and a
// resolving context whose Targets name that stack spell.
func TestTargetedByTargetCountsTheTargetsOfTheTargetedSpell(t *testing.T) {
	h, c := fixtureHost(t)
	big := h.g.AddObject(mkCard(t, "Name:Big Creature\nTypes:Creature\nPT:8/8\nOracle:x\n"), 0)
	h.g.Obj(big.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{big.ID})
	growth := h.g.AddObject(mkCard(t, "Name:Growth\nTypes:Instant\nOracle:x\n"), 0)
	h.g.Obj(growth.ID).Zone = state.ZStack
	h.g.SetZone(state.ZStack, 0, []state.ObjID{growth.ID})
	h.g.Obj(growth.ID).Targets = []state.Target{{Obj: big.ID}}
	c.Targets = []state.Target{{Obj: growth.ID}}

	// Preconditions: the inner spell really targets a controlled 8-power
	// battlefield creature, so the nested read has something to find, and
	// the printed face power is what the filter compares.
	inner := h.g.Obj(growth.ID)
	if inner == nil || inner.Zone != state.ZStack || len(inner.Targets) != 1 || inner.Targets[0].Obj != big.ID {
		t.Fatalf("precondition: stack spell does not carry the recorded target: %+v", inner)
	}
	tgt := h.g.Obj(big.ID)
	if tgt == nil || tgt.Zone != state.ZBattlefield || tgt.Controller != c.Controller {
		t.Fatalf("precondition: inner target is not a battlefield creature of the resolving controller: %+v", tgt)
	}
	if p := h.Power(big.ID); p != 8 {
		t.Fatalf("precondition: inner target power = %d, want 8", p)
	}

	const body = "TargetedByTarget$Valid Creature.powerGE7+YouCtrl"
	if n, ok := EvalCountOK(h, c, body); !ok || n != 1 {
		t.Errorf("%s = (%d, %v), want (1, true)", body, n, ok)
	}
}

func TestTargetedByTargetReadsZeroWhenTheFilterFails(t *testing.T) {
	h, c := fixtureHost(t)
	small := h.g.AddObject(mkCard(t, "Name:Small Creature\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	h.g.Obj(small.ID).Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{small.ID})
	growth := h.g.AddObject(mkCard(t, "Name:Growth\nTypes:Instant\nOracle:x\n"), 0)
	h.g.Obj(growth.ID).Zone = state.ZStack
	h.g.SetZone(state.ZStack, 0, []state.ObjID{growth.ID})
	h.g.Obj(growth.ID).Targets = []state.Target{{Obj: small.ID}, {Player: 1, IsPlayer: true}}
	c.Targets = []state.Target{{Obj: growth.ID}}

	// Preconditions: the same stack-spell binding, but the inner creature is
	// 2 power (below the GE7 threshold) and the spell's other target is a
	// player, which a Card... spec can never match.
	inner := h.g.Obj(growth.ID)
	if inner == nil || inner.Zone != state.ZStack || len(inner.Targets) != 2 || inner.Targets[0].Obj != small.ID || !inner.Targets[1].IsPlayer {
		t.Fatalf("precondition: stack spell does not carry the creature+player targets: %+v", inner)
	}
	if p := h.Power(small.ID); p != 2 {
		t.Fatalf("precondition: inner target power = %d, want 2 (must differ from the GE7 threshold)", p)
	}

	const body = "TargetedByTarget$Valid Creature.powerGE7+YouCtrl"
	if n, ok := EvalCountOK(h, c, body); !ok || n != 0 {
		t.Errorf("%s = (%d, %v), want (0, true): the power filter fails, so no reduction", body, n, ok)
	}
}

func TestTargetedByTargetEmptyBindingIsAModelledZero(t *testing.T) {
	h, c := fixtureHost(t)
	// No cast targets bound (the pre-fix cost path read exactly this shape):
	// a MODELLED head must resolve to a legitimate zero, never the
	// unresolvable verdict the Compare gate treats as an error condition.
	const body = "TargetedByTarget$Valid Creature.powerGE7+YouCtrl"
	if n, ok := EvalCountOK(h, c, body); !ok || n != 0 {
		t.Errorf("%s with no Ctx.Targets = (%d, %v), want (0, true)", body, n, ok)
	}
}

func TestTargetedByTargetUnknownPropertyFailsClosed(t *testing.T) {
	h, c := fixtureHost(t)
	// Only the Valid property is modelled (both corpus carriers use it); any
	// other property is the unresolvable verdict, and so is an empty spec.
	for _, body := range []string{"TargetedByTarget$Amount Creature.powerGE7", "TargetedByTarget$Valid "} {
		if n, ok := EvalCountOK(h, c, body); ok {
			t.Errorf("%s = (%d, %v), want unresolvable (fail closed)", body, n, ok)
		}
	}
}
