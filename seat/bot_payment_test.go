package seat

import (
	"context"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func paymentAction(id string, obj state.ObjID) decision.PaymentAction {
	return decision.PaymentAction{ID: id, Cast: decision.PlannedCast{Object: obj, Face: 0, Origin: "hand"}, Plans: []decision.PaymentPlan{{ID: id + "-plan", Version: decision.PaymentPlanV1}}}
}

func TestAutoPayManaSelectsPreferredPaymentInsteadOfManualActivation(t *testing.T) {
	bot := NewBot(19).EnableAutoPayMana()
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("pay", 42)},
	}
	brd := botpolicy.Board{IsMain: true, Cards: map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, CMC: 3, Castable: true},
	}}
	in := bot.decide(brd, &d)
	if in.Payment == nil {
		t.Fatalf("intent = %+v, want payment selection", in)
	}
	if in.Payment.ActionID != "pay" || in.Payment.Plan.ID != "pay-plan" {
		t.Fatalf("payment = %+v, want offered pay/pay-plan witness", in.Payment)
	}
	if len(in.Choices) != 0 {
		t.Fatalf("payment choices = %v, want no manual activation", in.Choices)
	}
}

func TestAutoPayManaKeepsLandBeforePayment(t *testing.T) {
	bot := NewBot(19).EnableAutoPayMana()
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "play_land", Obj: 1}, {Index: 2, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("pay", 42)},
	}
	brd := botpolicy.Board{IsMain: true, Cards: map[state.ObjID]botpolicy.Card{
		1:  {Basic: true, Castable: true},
		42: {Creature: true, Power: 3, CMC: 3, Castable: true},
	}}
	in := bot.decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("intent = %+v, want normal land play before payment", in)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("choices = %v, want play_land option 1", in.Choices)
	}
}

func TestAutoPayManaLeavesDecisionWithoutPlansUntouched(t *testing.T) {
	d := decision.Decision{Seq: 3, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "activate"}, {Index: 1, Kind: "pass"}},
	}
	v := view.View{Phase: "main1", Viewer: 0, Active: 0}
	plain, err := NewBot(23).Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	auto, err := NewBot(23).EnableAutoPayMana().Decide(context.Background(), v, d)
	if err != nil {
		t.Fatal(err)
	}
	if plain.Payment != nil || auto.Payment != nil || len(plain.Choices) != len(auto.Choices) {
		t.Fatalf("plain = %+v, auto no-plan = %+v", plain, auto)
	}
	for i := range plain.Choices {
		if plain.Choices[i] != auto.Choices[i] {
			t.Fatalf("plain = %+v, auto no-plan = %+v", plain, auto)
		}
	}
}
