package decision

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// consequenceFixture is paymentFixture's plan with a sacrifice step and a
// life:1 step (spec §4 amended: the last-resort consequence trailer).
func consequenceFixture(t *testing.T) (Decision, Intent) {
	t.Helper()
	d, in := paymentFixture(t)
	cast := d.PaymentActions[0].Cast
	plan := ClonePaymentPlan(in.Payment.Plan)
	plan.ID = ""
	plan.Activations[0].Consequence = &PaymentConsequence{Sacrifice: true}
	plan.Activations[1].Consequence = &PaymentConsequence{Life: 1}
	pid, err := PaymentPlanID(d.Seq, d.Player, cast, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.ID = pid
	d.PaymentActions[0].Plans = []PaymentPlan{plan}
	in.Payment.Plan = ClonePaymentPlan(plan)
	return d, in
}

func TestPaymentPlanConsequenceIdentityPinned(t *testing.T) {
	base, _ := paymentFixture(t)
	d, in := consequenceFixture(t)
	if err := d.Validate(in); err != nil {
		t.Fatalf("valid consequence selector: %v", err)
	}
	got := in.Payment.Plan.ID
	if got == base.PaymentActions[0].Plans[0].ID {
		t.Fatal("consequence trailer did not bind the plan identity")
	}
	// Computed independently from the codec note's byte layout (base vector
	// 3e5670…b5f1 followed by the consequences trailer).
	if want := "63d1f08ab0bd4c622c1f7b5ea063d26c8094968337a7ba6c8f7330901e444f81"; got != want {
		t.Fatalf("consequence plan vector = %s, want %s", got, want)
	}
	// The consequence is part of the witness: a changed consequence is a
	// different plan, whichever field changes.
	for name, change := range map[string]func(*PaymentConsequence){
		"life":           func(c *PaymentConsequence) { c.Life = 2 },
		"damage":         func(c *PaymentConsequence) { c.Damage = 1 },
		"no_untap":       func(c *PaymentConsequence) { c.NoUntap = true },
		"return_to_hand": func(c *PaymentConsequence) { c.ReturnToHand = true },
	} {
		t.Run(name, func(t *testing.T) {
			p := ClonePaymentPlan(in.Payment.Plan)
			p.ID = ""
			change(p.Activations[1].Consequence)
			id, err := PaymentPlanID(d.Seq, d.Player, d.PaymentActions[0].Cast, p)
			if err != nil {
				t.Fatal(err)
			}
			if id == got {
				t.Fatalf("changing %s kept plan identity %s", name, id)
			}
			ii := CloneIntent(in)
			change(ii.Payment.Plan.Activations[1].Consequence)
			if err := d.Validate(ii); err == nil {
				t.Fatalf("Validate accepted a witness whose %s consequence changed", name)
			}
		})
	}
	// Removing the consequence from the sacrifice step is a different plan too.
	ii := CloneIntent(in)
	ii.Payment.Plan.Activations[0].Consequence = nil
	if err := d.Validate(ii); err == nil {
		t.Fatal("Validate accepted a witness with its sacrifice disclosure removed")
	}
}

func TestPaymentPlanConsequenceShapeRejects(t *testing.T) {
	d, in := consequenceFixture(t)
	for name, c := range map[string]*PaymentConsequence{
		"zero valued":      {},
		"over-bound life":  {Life: ^uint32(0)},
		"over-bound dmg":   {Damage: maxPaymentQuantity + 1},
		"zero with bounds": {Life: 0, Damage: 0},
	} {
		t.Run(name, func(t *testing.T) {
			p := ClonePaymentPlan(in.Payment.Plan)
			p.Activations[1].Consequence = c
			if _, err := PaymentPlanID(d.Seq, d.Player, d.PaymentActions[0].Cast, p); err == nil {
				t.Fatalf("PaymentPlanID accepted consequence %+v", *c)
			}
			ii := CloneIntent(in)
			ii.Payment.Plan = p
			if err := d.Validate(ii); err == nil {
				t.Fatalf("Validate accepted consequence %+v", *c)
			}
		})
	}
}

func TestPaymentPlanConsequenceJSONRoundTrip(t *testing.T) {
	_, in := consequenceFixture(t)
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"consequence":{"sacrifice":true}`, `"consequence":{"life":1}`} {
		if !strings.Contains(s, want) {
			t.Fatalf("encoded intent lacks %s: %s", want, s)
		}
	}
	var back Intent
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, in) {
		t.Fatalf("round trip changed intent:\n got %#v\nwant %#v", back, in)
	}
	// A normal step carries no consequence key at all.
	d, _ := paymentFixture(t)
	b, err = json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "consequence") {
		t.Fatalf("normal plan encodes a consequence key: %s", b)
	}
}

func TestPaymentPlanConsequenceCloneDoesNotAlias(t *testing.T) {
	d, in := consequenceFixture(t)
	cp := ClonePaymentPlan(in.Payment.Plan)
	cp.Activations[1].Consequence.Life = 7
	if in.Payment.Plan.Activations[1].Consequence.Life != 1 {
		t.Fatal("ClonePaymentPlan aliases the consequence")
	}
	dd := d.Clone()
	dd.PaymentActions[0].Plans[0].Activations[0].Consequence.Sacrifice = false
	if !d.PaymentActions[0].Plans[0].Activations[0].Consequence.Sacrifice {
		t.Fatal("Decision.Clone aliases the consequence")
	}
	ci := CloneIntent(in)
	ci.Payment.Plan.Activations[0].Consequence.Damage = 3
	if in.Payment.Plan.Activations[0].Consequence.Damage != 0 {
		t.Fatal("CloneIntent aliases the consequence")
	}
}
