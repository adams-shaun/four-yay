package decision

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestRequiredQuotaIsDeterministicAcrossLifeValueTradeoffs guards the sparse
// DP's dominance rule: retaining a more expensive candidate for the same
// (life,count) state can make the quota depend on map iteration order.
func TestRequiredQuotaIsDeterministicAcrossLifeValueTradeoffs(t *testing.T) {
	d := &Decision{
		Kind: KAttackers, Max: 5, MaxSum: 4, Budgeted: true, PayerLife: 6,
		Options: []Option{
			{Index: 0, Kind: "attacker", Obj: 100, Required: true, CostLife: 3, Value: 2},
			{Index: 1, Kind: "attacker", Obj: 100, CostLife: 0, Value: 0},
			{Index: 2, Kind: "attacker", Obj: 101, CostLife: 0, CostPhyrexian: 1, Value: 0},
			{Index: 3, Kind: "attacker", Obj: 102, Required: true, CostLife: 3, Value: 0},
			{Index: 4, Kind: "attacker", Obj: 103, Required: true, CostLife: 0, Value: 3},
			{Index: 5, Kind: "attacker", Obj: 104, Required: true, CostLife: 1, Value: 0},
			{Index: 6, Kind: "attacker", Obj: 104, Required: true, CostLife: 3, Value: 2},
		},
	}
	if got, want := d.PayerLifeBound(), int32(6); got != want {
		t.Fatalf("precondition: payer life bound = %d, want %d", got, want)
	}
	witness := []int{3, 4, 5} // three distinct required objects; life 4, Value 3.
	if !d.ChargeOptionsFit(witness) {
		t.Fatal("precondition: the three-object witness must fit both published budgets")
	}
	if want := exhaustiveRequiredQuota(d); want != 3 {
		t.Fatalf("precondition: exhaustive quota = %d, want 3", want)
	}
	for i := 0; i < 200; i++ {
		if got := d.RequiredQuota(); got != 3 {
			t.Fatalf("RequiredQuota call %d = %d, want exact maximum 3", i+2, got)
		}
	}
}

// exhaustiveRequiredQuota is an independent all-combinations oracle for the
// small tradeoff fixture; it does not reuse the production sparse DP.
func exhaustiveRequiredQuota(d *Decision) int {
	groups := make([][]int, 0)
	positions := make(map[state.ObjID]int)
	for i := range d.Options {
		o := &d.Options[i]
		if !o.Required {
			continue
		}
		group, ok := positions[o.Obj]
		if !ok {
			group = len(groups)
			positions[o.Obj] = group
			groups = append(groups, nil)
		}
		groups[group] = append(groups[group], i)
	}
	best := 0
	var visit func(int, int, int, int32)
	visit = func(group, count, value int, life int32) {
		if group == len(groups) {
			if count > best {
				best = count
			}
			return
		}
		visit(group+1, count, value, life)
		if count >= d.maxChoices() {
			return
		}
		for _, index := range groups[group] {
			o := &d.Options[index]
			cost := o.chargeLifeCost()
			if life+cost > d.PayerLifeBound() || (d.HasBudget() && value+o.Value > d.MaxSum) {
				continue
			}
			visit(group+1, count+1, value+o.Value, life+cost)
		}
	}
	visit(0, 0, 0, 0)
	return best
}
