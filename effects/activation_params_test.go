package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestCompareOf pins the compiled comparison: the exact and case-folded
// operators, the literal right-hand side as written, and the SVar-named one
// kept as text.
func TestCompareOf(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		text     string
		op, fold CmpOp
		n        int
		lit      bool
	}{
		{"", "", CmpNone, CmpNone, 0, false},
		{"GE", "GE", CmpNone, CmpNone, 0, false},
		{" GE2 ", "GE2", CmpGE, CmpGE, 2, true},
		{"EQ0", "EQ0", CmpEQ, CmpEQ, 0, true},
		{"NE-1", "NE-1", CmpNE, CmpNE, -1, true},
		{"LTX", "LTX", CmpLT, CmpLT, 0, false},
		{"le3", "le3", CmpNone, CmpLE, 3, true},
		{"GT 3", "GT 3", CmpGT, CmpGT, 0, false},
		{"M23", "M23", CmpNone, CmpNone, 3, true},
	} {
		c := CompareOf(tc.raw)
		if c.Text != tc.text || c.Op != tc.op || c.Fold != tc.fold || c.N != tc.n || c.Lit != tc.lit {
			t.Errorf("CompareOf(%q) = %+v", tc.raw, c)
		}
	}
	if c := CompareOf("GE2"); !c.Holds(2) || c.Holds(1) || c.Rhs() != "2" {
		t.Errorf("GE2 holds/rhs wrong: %+v", c)
	}
	if c := CompareOf("GEX"); c.Holds(5) || c.Rhs() != "X" {
		t.Errorf("GEX must not hold as a literal: %+v", c)
	}
	if n := allocsPerRun(100, func() { _ = CompareOf(" ge12 ") }); n != 0 {
		t.Errorf("CompareOf allocates %v", n)
	}
}

// TestCompileActivation pins the compiled tier: costs and unless riders as
// written, the gates trimmed, the zone mask, the phase window and its flags,
// and the condition half.
func TestCompileActivation(t *testing.T) {
	sa := &cards.SA{API: "Draw", Params: map[string]string{
		"Cost": " T ", "Activation": " Threshold ", "ActivationZone": "Graveyard",
		"ActivationPhases": "Upkeep", "PlayerTurn": "true", "ActivationFirstCombat": "True",
		"ActivationLimit": "1", "Activator": " Player ", "SorcerySpeed": "True",
		"IsPresent": "Creature.YouCtrl", "PresentCompare": "GE2", "PresentZone": "Exile",
		"CheckSVar": "X", "SVarCompare": "LTY", "UnlessCost": " 2 ", "UnlessPayer": " TargetedController ",
		"UnlessSwitched": "True", "ConditionDefined": "Remembered", "ConditionPresent": "Card",
		"ConditionCompare": "EQ0", "ConditionPhases": "Main1,Main2", "ConditionZone": " Graveyard ",
	}}
	p := ActivationOf(sa)
	if p.Cost != " T " || p.Activation != "Threshold" || p.Activator.Text != "Player" || p.UnlessCost != " 2 " ||
		p.UnlessPayer != "TargetedController" || !p.Unless() {
		t.Fatalf("text fields = %+v", p)
	}
	if !p.ZoneOK(state.ZGraveyard) || p.ZoneOK(state.ZBattlefield) {
		t.Fatalf("zones = %b", p.Zones)
	}
	if !p.Phases.Present || !p.PhasesValid || !p.PhaseSet.Has(state.StepUpkeep) || p.PhaseSet.Has(state.StepDraw) {
		t.Fatalf("phases = %+v %v %b", p.Phases, p.PhasesValid, p.PhaseSet)
	}
	want := ActPlayerTurnTrue | ActFirstCombat | ActFirstCombatTrue | ActPhaseGate | ActSorcerySpeed | ActUnlessSwitched | ActConditionOther
	if p.Flags != want {
		t.Fatalf("flags = %b, want %b", p.Flags, want)
	}
	if !p.PresentCompare.Holds(2) || p.PresentZone.Text != "Exile" || p.CheckSVar.Text != "X" || p.SVarCompare.Lit {
		t.Fatalf("gates = %+v", p)
	}
	c := &p.Cond
	if c.Defined != "Remembered" || !c.Present.Present || c.Present.Text != "Card" || !c.Compare.Holds(0) ||
		!c.PhasesOK || !c.PhaseSet.Has(state.StepMain2) || !c.Any() {
		t.Fatalf("condition = %+v", c)
	}
	if z := conditionZoneParam(sa); z != "Graveyard" {
		t.Fatalf("ConditionZone = %q", z)
	}

	bare := ActivationOf(&cards.SA{API: "Draw", Params: map[string]string{"ConditionDescription": "x", "ActivationZone": "Command"}})
	if bare.Flags != 0 || bare.Zones != 0 || bare.Cond.Any() {
		t.Fatalf("bare = %+v", bare)
	}
	if empty := ActivationOf(nil); !empty.ZoneOK(state.ZBattlefield) || empty.Flags != 0 {
		t.Fatalf("empty = %+v", empty)
	}
	if n := allocsPerRun(100, func() { _ = ActivationOf(sa) }); n != 0 {
		t.Errorf("ActivationOf front-cache hit allocates %v", n)
	}
}
