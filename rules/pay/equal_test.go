package pay

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

func TestPayEqualCoversEveryField(t *testing.T) {
	for _, c := range []struct {
		v    any
		want int
	}{
		{PlanOutcome{}, planOutcomeFieldCount}, {decision.PaymentPlan{}, paymentPlanFieldCount},
		{decision.PaymentActivation{}, paymentActivationFieldCount},
	} {
		ty := reflect.TypeOf(c.v)
		if n := ty.NumField(); n != c.want {
			t.Errorf("%s has %d fields, equal.go covers %d", ty, n, c.want)
		}
	}
	for _, v := range []any{decision.PaymentCost{}, decision.PaymentAbility{}, decision.PaymentConsequence{}} {
		if !reflect.TypeOf(v).Comparable() {
			t.Errorf("%T is no longer comparable: equal.go compares it with ==", v)
		}
	}
}

func TestPayEqualMatchesDeepEqual(t *testing.T) {
	mk := func(m int) PlanOutcome {
		p := &decision.PaymentPlan{Version: 1, ID: "a", Activations: []decision.PaymentActivation{{Source: 3, Consequence: &decision.PaymentConsequence{Life: 1}}}}
		switch m {
		case 1:
			p.Activations[0].Consequence = &decision.PaymentConsequence{Life: 2}
		case 2:
			p.Activations[0].Consequence = nil
		case 3:
			p.Activations = []decision.PaymentActivation{}
		case 4:
			p.Activations = nil
		case 5:
			return PlanOutcome{Reason: "insufficient"}
		}
		return PlanOutcome{Plan: p, Nodes: 2}
	}
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			a, b := mk(i), mk(j)
			if got, want := PlanOutcomeEqual(a, b), reflect.DeepEqual(a, b); got != want {
				t.Errorf("PlanOutcomeEqual(%d,%d) = %v, DeepEqual %v", i, j, got, want)
			}
		}
	}
	ss := [][][]int{nil, {}, {nil}, {{}}, {{1, 2}}, {{1, 3}}}
	for i := range ss {
		for j := range ss {
			if got, want := intSlicesEqual(ss[i], ss[j]), reflect.DeepEqual(ss[i], ss[j]); got != want {
				t.Errorf("intSlicesEqual(%d,%d) = %v, DeepEqual %v", i, j, got, want)
			}
		}
	}
}
