package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestMixedStackAndPlainTargetGroupsStayInSlotOrder(t *testing.T) {
	reg := loadGenRegistry(t)
	stack := oraclegen.SlotInfos(faceOf(t, reg, "Bilbo's Gambit"))
	plain := oraclegen.SlotInfos(faceOf(t, reg, "Shock"))
	if len(stack) != 1 || !oraclegen.SlotIsStack(stack[0].Filter()) {
		t.Fatalf("precondition: Bilbo's Gambit slots = %+v, want one stack slot", stack)
	}
	if len(plain) != 1 || oraclegen.SlotIsStack(plain[0].Filter()) {
		t.Fatalf("precondition: Shock slots = %+v, want one plain slot", plain)
	}
	slots := append(stack, plain...)
	stackIdx := []int{0}
	fxs := oraclegen.Fixtures(plain)
	if len(fxs) == 0 {
		t.Fatal("precondition: Shock's plain target slot has no fixture")
	}
	fx := fxs[0]
	targets := insertStackTargets(slotFilters(slots), stackIdx, fx.Targets(), precast{card: "Shock"})
	if len(targets) != 2 || targets[0] != "p0:Shock" {
		t.Fatalf("precondition: full target order = %v, want stack target then plain target", targets)
	}
	sc := buildStackScenario(faceOf(t, reg, "Bilbo's Gambit"), "Bilbo's Gambit", "U", precast{card: "Shock"}, fx, slots, stackIdx, nil)
	if len(sc.Steps) != 2 {
		t.Fatalf("precondition: scenario has %d steps, want precast plus tested cast", len(sc.Steps))
	}
	got := sc.Steps[1].TargetGroups
	if len(got) != len(targets) {
		t.Fatalf("groups = %+v, want one group per target/slot %v", got, targets)
	}
	for i, want := range targets {
		if len(got[i].Picks) != 1 || got[i].Picks[0] != want {
			t.Errorf("group %d = %+v, want pick %q", i, got[i], want)
		}
	}
	if got[0].Max != stack[0].Max() || got[1].Max != plain[0].Max() {
		t.Errorf("group maxima = [%d %d], want [%d %d]", got[0].Max, got[1].Max, stack[0].Max(), plain[0].Max())
	}
}
