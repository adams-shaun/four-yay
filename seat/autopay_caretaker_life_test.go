package seat

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// caretakerLifeDecision offers exactly one payment action whose plan pays
// life (a Mana Confluence step), beside a manual activation and pass.
func caretakerLifeDecision(life, damage uint32) (decision.Decision, botpolicy.Board) {
	a := paymentAction("pay", 42)
	a.Plans[0].Activations = []decision.PaymentActivation{{Source: 5, Ability: decision.PaymentAbility{Kind: decision.PaymentAbilityPrinted},
		Produces: decision.ManaAmount{0, 1, 0, 0, 0, 0}, Consequence: &decision.PaymentConsequence{Life: life, Damage: damage}}}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate", Obj: 5}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{a},
	}
	brd := botpolicy.Board{IsMain: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, CMC: 3, Castable: true},
	})}
	return d, brd
}

// Spec §6 / aph-last-resort-plans: a caretaker standing in for a human never
// auto-selects a life-paying plan; it answers from the ordinary options.
// The same bot without the option (a hosted bot or tool seat) submits it.
func TestAutoPayCaretakerSkipsLifePayingPlan(t *testing.T) {
	d, brd := caretakerLifeDecision(1, 0)
	in := NewBot(19).EnableAutoPayMana().SkipLifePlans().decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("caretaker submitted life-paying plan %+v", in.Payment)
	}
	if err := d.Validate(in); err != nil {
		t.Fatalf("caretaker's legacy intent %+v is not a valid answer: %v", in, err)
	}

	d, brd = caretakerLifeDecision(1, 0)
	in = NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" || in.Payment.Plan.ID != "pay-plan" {
		t.Fatalf("hosted bot intent = %+v, want the offered life-paying plan", in)
	}
}

// Only life needs confirmation: a caretaker still takes a plan whose only
// consequence is damage (or a sacrifice), which the client discloses without
// a confirmation step.
func TestAutoPayCaretakerKeepsNonLifeLastResortPlan(t *testing.T) {
	d, brd := caretakerLifeDecision(0, 2)
	in := NewBot(19).EnableAutoPayMana().SkipLifePlans().decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("caretaker intent = %+v, want the damage-only plan", in)
	}
}
