package botpolicy

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestClampKeepsRequiredAttackersChargeFeasible is the review-r1 MAJOR's
// policy-layer pin: Clamp's repair (FitRequired) must build a required
// KAttackers answer that is COMBINED-CHARGE-feasible, not restore a required
// option whose pip overruns the published life bound. Three required pips at
// four life: the quota is two and Clamp must hand back exactly two -- the
// engine's Submit would reject a third and host.runMatch would crash the
// table.
func TestClampKeepsRequiredAttackersChargeFeasible(t *testing.T) {
	d := &decision.Decision{Kind: decision.KAttackers, Player: 0, Min: 0, Max: 3, PayerLife: 4,
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Obj: 1, Required: true, CostPhyrexian: 1},
			{Index: 1, Kind: "attacker", Obj: 2, Required: true, CostPhyrexian: 1},
			{Index: 2, Kind: "attacker", Obj: 3, Required: true, CostPhyrexian: 1},
		}}
	// Preconditions: the bound binds, and the unpayable pick is genuinely the
	// one repair has to drop.
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("fixture wrong: RequiredQuota = %d, want 2", q)
	}
	if d.ChargeOptionsFit([]int{0, 1, 2}) {
		t.Fatal("fixture wrong: three pips must not fit four life")
	}
	in := Clamp(d, decision.Intent{Player: 0, Choices: []int{0, 1, 2}})
	if err := d.Validate(in); err != nil {
		t.Fatalf("clamped answer %v failed Validate: %v", in.Choices, err)
	}
	if !d.ChargeOptionsFit(in.Choices) {
		t.Fatalf("clamped answer %v is not charge-feasible (life %d)", in.Choices, d.PayerLife)
	}
	if n := d.RequiredChosen(in.Choices); n < d.RequiredQuota() {
		t.Fatalf("clamped answer %v covers %d required, quota is %d", in.Choices, n, d.RequiredQuota())
	}
	if !reflect.DeepEqual(in.Choices, []int{0, 1}) {
		t.Fatalf("clamped choices = %v, want [0 1] (the third pip is unpayable)", in.Choices)
	}
}
