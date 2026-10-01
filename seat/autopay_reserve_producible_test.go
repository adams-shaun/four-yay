package seat

// Test for autopay-bot-reserve-producible: C7 (the mana reserve) must fire on
// a plan-backed cast. Under the auto-pay adapter every candidate cast is a
// plan decision and a V1 plan is offered only when the floating pool is EMPTY
// (rules/payment_plan.go's paymentPlanPoolOK), so the cast scorer's
// ctx.poolTotal is always 0 and C7's `poolTotal-cost >= res` can never hold:
// the auto-pay bot never prefers the cast that keeps its instant castable,
// while the manual bot (which floats mana before casting) does.
//
// The fix prices C7 -- and the pool-reading ManaLeft feature -- against
// ctx.producible (the pool plus untapped sources' guaranteed production) for
// a candidate the adapter marks plan-backed (decision.Option.PlanBacked),
// never inferred from Pool == 0, so the manual path is byte-identical.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAutoPayReserveUsesProducibleMana is the brief's headline case: four
// untapped Forests, two equal-power creatures -- 43 (offered first) costs
// {3}{G} and empties the board, 42 costs {1}{G} and keeps the {1}{G} instant
// 44 (the reserve, 2) castable. The manual bot floats the four green and
// casts 42 (C7 +10); auto-pay must do the same, priced against the
// four-producible board rather than the empty pool.
func TestAutoPayReserveUsesProducibleMana(t *testing.T) {
	cardsMap := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(), 3: forestCard(), 4: forestCard(),
		43: {Creature: true, Power: 2, Toughness: 2, CMC: 4, Castable: true, ManaCost: "3 G"},
		42: {Creature: true, Power: 2, Toughness: 2, CMC: 2, Castable: true, ManaCost: "1 G"},
		44: {CMC: 2, Castable: true, ManaCost: "1 G", InstantSpeed: true},
	}

	// Precondition: the manual bot (four mana floated, both casts offered as
	// legacy options) keeps the reserve and casts 42. If this ever stops
	// holding the auto-pay assertion below is meaningless.
	manualBrd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(cardsMap)}
	manualBrd.Pool[state.MG] = 4
	md := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "cast", Obj: 43}, {Index: 1, Kind: "cast", Obj: 42}, {Index: 2, Kind: "pass"}}}
	mi := NewBot(19).decide(manualBrd, &md)
	if len(mi.Choices) != 1 || md.Options[mi.Choices[0]].Obj != 42 {
		t.Fatalf("precondition: the manual bot should keep the reserve (cast 42), got %+v", mi)
	}

	// The auto-pay board: an EMPTY pool (the plan path's precondition) but
	// four untapped Forests, so producible mana is 4 -- which is what C7 must
	// read for a plan-backed candidate.
	autoBrd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(cardsMap)}
	if got := autoBrd.Pool.Total(); got != 0 {
		t.Fatalf("precondition: the auto-pay pool must be empty (a V1 plan is offered only then), got %d", got)
	}
	// producible mana = pool + every untapped source's guaranteed production.
	producible := autoBrd.Pool.Total()
	for _, c := range cardsMap {
		if !c.OnBattlefield || c.Tapped {
			continue
		}
		for _, n := range c.Produces.Colour {
			producible += n
		}
	}
	if producible != 4 {
		t.Fatalf("precondition: producible mana = %d, want 4 (four untapped Forests)", producible)
	}
	// reserve = the cheapest castable instant-speed card's CMC (0 when none).
	var reserve int32
	for _, c := range cardsMap {
		if !c.Castable || !c.InstantSpeed || c.CMC <= 0 {
			continue
		}
		if reserve == 0 || c.CMC < reserve {
			reserve = c.CMC
		}
	}
	if reserve != 2 {
		t.Fatalf("precondition: reserve = %d, want 2 (the {1}{G} instant in hand)", reserve)
	}

	ad := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("a43", 43), paymentAction("a42", 42)},
	}
	ai := NewBot(19).EnableAutoPayMana().decide(autoBrd, &ad)
	if ai.Payment == nil || ai.Payment.ActionID != "a42" {
		t.Fatalf("auto-pay did not keep the instant reserve: intent %+v (want plan a42)", ai)
	}
}

// TestAutoPayReserveStaysInertWithoutReserve is the control: with no
// instant-speed card in hand the reserve is 0, so C7 is inert on both
// surfaces and the plan-backed pricing must not invent a preference. It pins
// that the flag changes C7's reading, not the tie rule.
func TestAutoPayReserveStaysInertWithoutReserve(t *testing.T) {
	cardsMap := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(), 3: forestCard(), 4: forestCard(),
		43: {Creature: true, Power: 2, Toughness: 2, CMC: 4, Castable: true, ManaCost: "3 G"},
		42: {Creature: true, Power: 2, Toughness: 2, CMC: 2, Castable: true, ManaCost: "1 G"},
	}
	autoBrd := botpolicy.Board{IsMain: true, MyTurn: true, Cards: botpolicy.TableOf(cardsMap)}
	var reserve int32
	for _, c := range cardsMap {
		if !c.Castable || !c.InstantSpeed || c.CMC <= 0 {
			continue
		}
		if reserve == 0 || c.CMC < reserve {
			reserve = c.CMC
		}
	}
	if reserve != 0 {
		t.Fatalf("precondition: reserve = %d, want 0 with no instant-speed card in hand", reserve)
	}
	ad := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options:        []decision.Option{{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "pass"}},
		PaymentActions: []decision.PaymentAction{paymentAction("a43", 43), paymentAction("a42", 42)},
	}
	ai := NewBot(19).EnableAutoPayMana().decide(autoBrd, &ad)
	if ai.Payment == nil || ai.Payment.ActionID != "a43" {
		t.Fatalf("intent %+v, want the option-order tie to keep plan a43 when the reserve is 0", ai)
	}
}
