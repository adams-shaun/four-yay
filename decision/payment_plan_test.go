package decision

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func paymentFixture(t *testing.T) (Decision, Intent) {
	t.Helper()
	cast := PlannedCast{Object: 17, Face: 0, Origin: "hand"}
	plan := PaymentPlan{
		Version: PaymentPlanV1,
		Cost:    PaymentCost{Generic: 1, Mana: ManaAmount{0, 1, 1, 0, 0, 0}},
		Activations: []PaymentActivation{
			{Source: 41, SourceZoneSeq: GenesisZoneSeq, Ability: PaymentAbility{Kind: PaymentAbilityIntrinsic, Intrinsic: "basic_land"}, Produces: ManaAmount{0, 1, 0, 0, 0, 0}},
			{Source: 42, SourceZoneSeq: 99, Ability: PaymentAbility{Kind: PaymentAbilityPrinted, Face: 0, Index: 2}, Produces: ManaAmount{0, 0, 1, 0, 0, 0}},
		},
		PoolSpend: ManaAmount{0, 0, 0, 0, 0, 1},
		PoolAfter: ManaAmount{0, 0, 0, 0, 0, 0},
	}
	pid, err := PaymentPlanID(123, 0, cast, plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.ID = pid
	aid, err := PaymentActionID(PaymentPlanV1, 123, 0, cast)
	if err != nil {
		t.Fatal(err)
	}
	d := Decision{Seq: 123, Player: 0, Kind: KPriority, Min: 1, Max: 1,
		PaymentActions: []PaymentAction{{ID: aid, Cast: cast, Label: "display only", Plans: []PaymentPlan{plan}}}}
	return d, Intent{Seq: 123, Player: 0, Payment: &PaymentSelection{ActionID: aid, Plan: plan}}
}

func TestPaymentPlanValidateExclusiveSelector(t *testing.T) {
	d, in := paymentFixture(t)
	if err := d.Validate(in); err != nil {
		t.Fatalf("valid payment selector: %v", err)
	}
	for name, change := range map[string]func(*Decision, *Intent){
		"mixed choices": func(_ *Decision, in *Intent) { in.Choices = []int{0} },
		"rest":          func(_ *Decision, in *Intent) { in.Rest = []int{0} },
		"wrong kind":    func(d *Decision, _ *Intent) { d.Kind = KTarget },
		"unoffered id":  func(_ *Decision, in *Intent) { in.Payment.ActionID = "nope" },
		"changed plan":  func(_ *Decision, in *Intent) { in.Payment.Plan.PoolAfter[0]++ },
		"unknown version": func(_ *Decision, in *Intent) {
			in.Payment.Plan.Version = 2
		},
		"duplicate source": func(_ *Decision, in *Intent) {
			in.Payment.Plan.Activations[1].Source = in.Payment.Plan.Activations[0].Source
		},
		"unknown ability": func(_ *Decision, in *Intent) {
			in.Payment.Plan.Activations[0].Ability.Kind = "granted"
		},
		"overflow": func(_ *Decision, in *Intent) { in.Payment.Plan.Cost.Generic = ^uint32(0) },
	} {
		t.Run(name, func(t *testing.T) {
			dd := d.Clone()
			ii := CloneIntent(in)
			change(dd, &ii)
			if err := dd.Validate(ii); err == nil {
				t.Fatal("Validate accepted malformed payment selector")
			}
		})
	}
	tooMany := d.Clone()
	tooMany.PaymentActions[0].Plans[0].Activations = make([]PaymentActivation, MaxPaymentActivations+1)
	for i := range tooMany.PaymentActions[0].Plans[0].Activations {
		tooMany.PaymentActions[0].Plans[0].Activations[i] = PaymentActivation{Source: state.ObjID(i + 1), Ability: PaymentAbility{Kind: PaymentAbilityIntrinsic, Intrinsic: "basic_land"}}
	}
	large := CloneIntent(in)
	large.Payment.Plan = tooMany.PaymentActions[0].Plans[0]
	if err := tooMany.Validate(large); err == nil {
		t.Fatal("Validate accepted oversized payment plan")
	}
}

func TestPaymentPlanLegacyValidationUnchanged(t *testing.T) {
	d := Decision{Seq: 5, Player: 1, Kind: KPriority, Min: 1, Max: 1, Options: []Option{{Index: 0}}}
	if err := d.Validate(Intent{Seq: 5, Player: 1, Choices: []int{0}}); err != nil {
		t.Fatalf("legacy valid answer changed: %v", err)
	}
	if err := d.Validate(Intent{Seq: 5, Player: 1, Choices: nil}); err == nil {
		t.Fatal("legacy empty Choices became valid")
	}
}

func TestPaymentPlanIdentityIsIndependentOfPresentation(t *testing.T) {
	d, in := paymentFixture(t)
	firstAction := d.PaymentActions[0].ID
	firstPlan := in.Payment.Plan.ID
	d.PaymentActions[0].Label = "renamed"
	d.PaymentActions[0].BaseOptionIndex = new(int)
	d.PaymentActions = append([]PaymentAction{{ID: "another", Label: "other"}}, d.PaymentActions...)
	if got := d.PaymentActions[1].ID; got != firstAction {
		t.Fatalf("presentation changed action identity: %s != %s", got, firstAction)
	}
	if got := in.Payment.Plan.ID; got != firstPlan {
		t.Fatalf("presentation changed plan identity: %s != %s", got, firstPlan)
	}
	// Pin the documented canonical vector: IDs are full lower-case SHA-256.
	if got, want := firstAction, "7cd7d94c50da5e37389ac770de705e044430bd2ba4b8067202b36fe812f39ee4"; got != want {
		t.Fatalf("action vector = %s, want %s", got, want)
	}
	if got, want := firstPlan, "3e5670814bfa0bd0d71551dd79dcbbaa8b04212bd52bbfc8305a68ba5dbbb5f1"; got != want {
		t.Fatalf("plan vector = %s, want %s", got, want)
	}
}

func TestPaymentPlanCopiesOwnWitness(t *testing.T) {
	d, in := paymentFixture(t)
	i := 3
	d.PaymentActions[0].BaseOptionIndex = &i
	cp := d.Clone()
	cp.PaymentActions[0].Label = "changed"
	cp.PaymentActions[0].Plans[0].Activations[0].Produces[1] = 9
	*cp.PaymentActions[0].BaseOptionIndex = 7 // allocated only when non-nil below
	if d.PaymentActions[0].Label != "display only" || d.PaymentActions[0].Plans[0].Activations[0].Produces[1] != 1 || *d.PaymentActions[0].BaseOptionIndex != 3 {
		t.Fatal("decision clone aliases payment action")
	}
	ci := CloneIntent(in)
	ci.Payment.Plan.Activations[0].Produces[1] = 8
	if in.Payment.Plan.Activations[0].Produces[1] != 1 {
		t.Fatal("intent clone aliases payment witness")
	}
}

func TestPaymentPlanClonePreservesExplicitEmptyActivations(t *testing.T) {
	p := PaymentPlan{Version: PaymentPlanV1, Activations: make([]PaymentActivation, 0)}
	got := ClonePaymentPlan(p)
	if got.Activations == nil || len(got.Activations) != 0 {
		t.Fatalf("cloned activations = %#v, want explicit empty slice", got.Activations)
	}
}

func TestPaymentPlanOptionalJSONAndQuantityDecode(t *testing.T) {
	b, err := json.Marshal(Decision{Seq: 1, Player: 0, Kind: KPriority, Options: []Option{}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "payment_") {
		t.Fatalf("absent payment fields changed legacy JSON: %s", b)
	}
	var in Intent
	if err := json.Unmarshal([]byte(`{"seq":1,"player":0,"choices":[],"payment":{"action_id":"a","plan":{"version":1,"id":"x","cost":{"generic":-1,"mana":[0,0,0,0,0,0]},"activations":[],"pool_spend":[0,0,0,0,0,0],"pool_after":[0,0,0,0,0,0]}}}`), &in); err == nil {
		t.Fatal("negative quantity decoded into payment witness")
	}
}
