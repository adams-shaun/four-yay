package seat

// The auto-pay adapter must submit the cast mode the policy chose. When a
// decision offers a legacy cast option with a non-ordinary shape (an evoke,
// pitch, dash, surge or other alternative mode the empty pool can already
// pay) AND a payment plan for the same object's ordinary cast, the candidate
// must carry BOTH entries and map the pick back by candidate option
// identity: a pick of the legacy mode is submitted as itself, and only a
// pick of the plan-only entry (or of a legacy ordinary cast) pays the plan.
// Measured on the audit harness: 23 mode substitutions in 1,050 commander
// games before the fix (e:mode_substituted_by_plan).

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// evokedCastBoard returns a board whose only castable card (42, a
// Solitude-like creature with a 5-mana ordinary cost and a cheap evoke cost)
// is offered BOTH as a legacy cast option (Mode "evoked", payable from the
// empty pool) and as a payment plan for its ordinary cast. The board carries
// no mana sources so the tap gate never redirects the window (tapWants finds
// no source to tap).
func evokedCastBoard() (botpolicy.Board, decision.Decision) {
	brd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, Toughness: 2, CMC: 5, Castable: true, ManaCost: "3 W W"},
	})}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1},
			{Index: 1, Kind: "cast", Obj: 42, Mode: "evoked", Label: "Cast Solitude (evoked)"},
			{Index: 2, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("hard", 42)},
	}
	return brd, d
}

// The policy picks the legacy evoked cast (option 1); the adapter must submit
// that exact option -- Payment nil, Choices [1] -- and never the ordinary
// plan. With the substitution bug the intent carried the "hard" plan.
func TestAutoPayKeepsChosenLegacyMode(t *testing.T) {
	brd, d := evokedCastBoard()
	manual := NewBot(19).decide(brd, &d)
	if len(manual.Choices) != 1 || d.Options[manual.Choices[0]].Obj != 42 {
		t.Fatalf("precondition: the manual policy should prefer casting 42, got %+v", manual)
	}
	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment != nil {
		t.Fatalf("the policy chose option 1 (evoked) but the adapter submitted the plan %s", in.Payment.ActionID)
	}
	if len(in.Choices) != 1 || in.Choices[0] != 1 {
		t.Fatalf("choices = %v, want the chosen legacy mode option 1", in.Choices)
	}
}

// The same decision, but the evoke cost is not payable this turn so the
// engine offers no legacy cast for 42: the plan-only ordinary entry (visible
// alongside the legacy options) is then the best pick, and the adapter pays
// the plan. A regression here would strand the planned spell.
func TestAutoPayPaysPlanWhenPlanOnlyEntryIsBest(t *testing.T) {
	brd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, Toughness: 2, CMC: 5, Castable: true, ManaCost: "3 W W"},
	})}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1},
			{Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("hard", 42)},
	}
	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil {
		t.Fatalf("intent = %+v, want the plan-only entry's payment", in)
	}
	if in.Payment.ActionID != "hard" || in.Payment.Plan.ID != "hard-plan" {
		t.Fatalf("payment = %+v, want the offered hard/hard-plan witness", in.Payment)
	}
	if len(in.Choices) != 0 {
		t.Fatalf("choices = %v, want no manual activation", in.Choices)
	}
}

// A legacy ORDINARY cast pick (Mode "" && AltCostIndex == 0) of a planned
// object also pays the plan -- the ordinary cast the plan witnesses is the
// cast the policy chose -- so the two ordinary candidates can never diverge.
func TestAutoPayPaysPlanWhenLegacyOrdinaryCastChosen(t *testing.T) {
	brd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(map[state.ObjID]botpolicy.Card{
		42: {Creature: true, Power: 3, Toughness: 2, CMC: 3, Castable: true, ManaCost: "2 W"},
	})}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1},
			{Index: 1, Kind: "cast", Obj: 42, Label: "Cast Solitude"},
			{Index: 2, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("hard", 42)},
	}
	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil {
		t.Fatalf("intent = %+v, want the plan paid for the legacy ordinary cast", in)
	}
	if in.Payment.ActionID != "hard" || in.Payment.Plan.ID != "hard-plan" {
		t.Fatalf("payment = %+v, want the offered hard/hard-plan witness", in.Payment)
	}
	if len(in.Choices) != 0 {
		t.Fatalf("choices = %v, want no manual activation", in.Choices)
	}
}
