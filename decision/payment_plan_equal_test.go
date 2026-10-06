package decision

import (
	"reflect"
	"testing"
)

func TestPaymentPlanEqualMatchesDeepEqual(t *testing.T) {
	for typ, n := range map[reflect.Type]int{
		reflect.TypeFor[PaymentPlan]():        6,
		reflect.TypeFor[PaymentActivation]():  5,
		reflect.TypeFor[PaymentCost]():        2,
		reflect.TypeFor[PaymentAbility]():     4,
		reflect.TypeFor[PaymentConsequence](): 5,
	} {
		if typ.NumField() != n {
			t.Fatalf("%s has %d fields, payment_plan_equal.go was written against %d: extend it", typ, typ.NumField(), n)
		}
	}
	act := func() PaymentActivation {
		return PaymentActivation{Source: 3, SourceZoneSeq: 9, Ability: PaymentAbility{Kind: "printed", Face: 1, Index: 2}, Produces: ManaAmount{0, 1}, Consequence: &PaymentConsequence{Life: 1}}
	}
	base := func() PaymentPlan {
		return PaymentPlan{Version: 1, ID: "p", Cost: PaymentCost{Generic: 1, Mana: ManaAmount{1}}, Activations: []PaymentActivation{act()}, PoolSpend: ManaAmount{0, 0, 1}, PoolAfter: ManaAmount{2}}
	}
	muts := []func(*PaymentPlan){
		func(p *PaymentPlan) {},
		func(p *PaymentPlan) { p.Version = 2 },
		func(p *PaymentPlan) { p.ID = "q" },
		func(p *PaymentPlan) { p.Cost.Generic = 0 },
		func(p *PaymentPlan) { p.Cost.Mana[5] = 1 },
		func(p *PaymentPlan) { p.Activations = nil },
		func(p *PaymentPlan) { p.Activations = []PaymentActivation{} },
		func(p *PaymentPlan) { p.Activations = append(p.Activations, act()) },
		func(p *PaymentPlan) { p.Activations[0].Source = 4 },
		func(p *PaymentPlan) { p.Activations[0].SourceZoneSeq = 1 },
		func(p *PaymentPlan) { p.Activations[0].Ability.Intrinsic = "basic_land" },
		func(p *PaymentPlan) { p.Activations[0].Produces[2] = 1 },
		func(p *PaymentPlan) { p.Activations[0].Consequence = nil },
		func(p *PaymentPlan) { p.Activations[0].Consequence = &PaymentConsequence{Life: 1, Sacrifice: true} },
		func(p *PaymentPlan) { p.Activations[0].Consequence = &PaymentConsequence{Life: 1} }, // equal pointee, new pointer
		func(p *PaymentPlan) { p.PoolSpend[0] = 7 },
		func(p *PaymentPlan) { p.PoolAfter[1] = 7 },
	}
	plans := make([]PaymentPlan, len(muts))
	for i, m := range muts {
		plans[i] = base()
		m(&plans[i])
	}
	for i := range plans {
		for j := range plans {
			if got, want := plans[i].Equal(&plans[j]), reflect.DeepEqual(plans[i], plans[j]); got != want {
				t.Fatalf("plans %d, %d: Equal %v, DeepEqual %v", i, j, got, want)
			}
		}
		c := ClonePaymentPlan(plans[i])
		if !plans[i].Equal(&c) {
			t.Fatalf("plan %d differs from its clone", i)
		}
	}
}
