package decision

import (
	"reflect"
	"testing"
)

// TestChargeOptionConstraintsDropsTapObligations pins the tap half of the
// shared combat-charge answer rule: a positive CostTaps is an obligation the
// wire cannot let the answer verify, so the option is dropped.
func TestChargeOptionConstraintsDropsTapObligations(t *testing.T) {
	d := &Decision{Kind: KAttackers, Options: []Option{
		{Index: 0, Obj: 1},
		{Index: 1, Obj: 2, CostTaps: 1},
	}}
	if got := ChargeOptionConstraints(d, []int{0, 1}, 20, 0); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("choices = %v, want [0] (tap-costed option dropped)", got)
	}
}

// TestChargeOptionConstraintsTrimsCumulativeLife pins the life half: the
// cumulative CostLife of the kept options must not exceed the acting
// player's life total, earliest kept.
func TestChargeOptionConstraintsTrimsCumulativeLife(t *testing.T) {
	d := &Decision{Kind: KAttackers, Options: []Option{
		{Index: 0, Obj: 1, CostLife: 2},
		{Index: 1, Obj: 2, CostLife: 2},
	}}
	if got := ChargeOptionConstraints(d, []int{0, 1}, 3, 0); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("choices = %v, want [0] (second would exceed 3 life)", got)
	}
}

// TestChargeOptionConstraintsHonoursManaBudget pins the optional MaxSum term
// the KBlockers arm passes: the cumulative Value must not exceed maxSum.
func TestChargeOptionConstraintsHonoursManaBudget(t *testing.T) {
	d := &Decision{Kind: KBlockers, Options: []Option{
		{Index: 0, Obj: 1, Value: 2},
		{Index: 1, Obj: 2, Value: 2},
	}}
	if got := ChargeOptionConstraints(d, []int{0, 1}, 20, 3); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("choices = %v, want [0] (second would exceed mana 3)", got)
	}
	if got := ChargeOptionConstraints(d, []int{0, 1}, 20, 0); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want both when no mana budget is published", got)
	}
}
