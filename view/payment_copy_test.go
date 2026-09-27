package view

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestCopyDecisionPaymentOwnership(t *testing.T) {
	baseOption := 3
	d := &decision.Decision{
		Options: []decision.Option{{Index: 0, Label: "Mountain: CARDNAME"}},
		PaymentActions: []decision.PaymentAction{{
			ID:              "action",
			BaseOptionIndex: &baseOption,
			Plans: []decision.PaymentPlan{{
				ID:          "plan",
				Activations: []decision.PaymentActivation{{Source: state.ObjID(9)}},
			}},
		}},
		PaymentFallback: &decision.PaymentFallback{PlanID: "fallback", Reason: "original"},
	}
	if d.PaymentActions[0].BaseOptionIndex == nil || d.PaymentFallback == nil ||
		d.PaymentActions[0].Plans[0].Activations == nil || len(d.Options) != 1 {
		t.Fatal("fixture does not exercise all decision-copy ownership fields")
	}
	if d.Options[0].Label == "Mountain: Mountain" || d.PaymentActions[0].Plans[0].Activations[0].Source == 10 {
		t.Fatal("fixture comparison values unexpectedly equal")
	}

	cp := copyDecision(d)
	if cp == nil || cp.PaymentActions[0].BaseOptionIndex == nil || cp.PaymentFallback == nil {
		t.Fatal("copyDecision omitted payment extension")
	}
	if cp.Options[0].Label != "Mountain: Mountain" {
		t.Errorf("projected option label = %q, want substituted label", cp.Options[0].Label)
	}
	if d.Options[0].Label != "Mountain: CARDNAME" {
		t.Errorf("projection changed original option label: %q", d.Options[0].Label)
	}

	*cp.PaymentActions[0].BaseOptionIndex = 8
	cp.PaymentActions[0].Plans[0].Activations[0].Source = 10
	cp.PaymentFallback.Reason = "projected"
	cp.Options[0].Label = "projected option"

	if *cp.PaymentActions[0].BaseOptionIndex != 8 || cp.PaymentActions[0].Plans[0].Activations[0].Source != 10 ||
		cp.PaymentFallback.Reason != "projected" || cp.Options[0].Label != "projected option" {
		t.Fatal("projected fields did not take their mutation values")
	}
	if *d.PaymentActions[0].BaseOptionIndex != 3 {
		t.Errorf("projected BaseOptionIndex mutation reached original: %d", *d.PaymentActions[0].BaseOptionIndex)
	}
	if got := d.PaymentActions[0].Plans[0].Activations[0].Source; got != 9 {
		t.Errorf("projected activation mutation reached original: source = %d", got)
	}
	if got := d.PaymentFallback.Reason; got != "original" {
		t.Errorf("projected fallback mutation reached original: reason = %q", got)
	}
	if got := d.Options[0].Label; got != "Mountain: CARDNAME" {
		t.Errorf("projected option mutation reached original: label = %q", got)
	}
}
