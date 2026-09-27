package decision

import (
	"reflect"
	"testing"
)

// TestChargeOptionConstraintsPricesPhyrexianPips pins the pip half of the
// shared combat-charge answer rule: each CostPhyrexian pip is priced at its
// life branch (two life, CR 107.4f) and folded into the SAME cumulative life
// bound as CostLife, so a declaration of individually-affordable pip-taxed
// pairs (Norn's Annex at low life) is trimmed to what the payer can cover.
func TestChargeOptionConstraintsPricesPhyrexianPips(t *testing.T) {
	d := &Decision{Kind: KAttackers, Options: []Option{
		{Index: 0, Obj: 1, CostPhyrexian: 1},
		{Index: 1, Obj: 2, CostPhyrexian: 1},
		{Index: 2, Obj: 3, CostPhyrexian: 1},
	}}
	// 4 life funds exactly two pips (2 x 2 life); the third overruns.
	if got := ChargeOptionConstraints(d, []int{0, 1, 2}, 4, 0); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want [0 1] (third pip exceeds 4 life)", got)
	}
	// 6 life funds all three.
	if got := ChargeOptionConstraints(d, []int{0, 1, 2}, 6, 0); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("choices = %v, want all three at 6 life", got)
	}
	// A pip and a fixed life charge SHARE the one bound: 2 life + 1 pip = 4.
	mixed := &Decision{Kind: KAttackers, Options: []Option{
		{Index: 0, Obj: 1, CostLife: 2},
		{Index: 1, Obj: 2, CostPhyrexian: 1},
		{Index: 2, Obj: 3, CostPhyrexian: 1},
	}}
	if got := ChargeOptionConstraints(mixed, []int{0, 1, 2}, 5, 0); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want [0 1] (2 + 2 = 4 of 5 life; third pip overruns)", got)
	}
}

// TestChargeOptionConstraintsExemptsPhyrexianWithUnpublishedLife pins the
// negative-life contract: a life total the wire did not publish (negative)
// leaves the life bound inert, as it does for CostLife.
func TestChargeOptionConstraintsExemptsPhyrexianWithUnpublishedLife(t *testing.T) {
	d := &Decision{Kind: KAttackers, Options: []Option{
		{Index: 0, Obj: 1, CostPhyrexian: 1},
		{Index: 1, Obj: 2, CostPhyrexian: 1},
	}}
	if got := ChargeOptionConstraints(d, []int{0, 1}, -1, 0); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("choices = %v, want both when life is unpublished", got)
	}
}
