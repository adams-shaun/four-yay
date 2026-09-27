package seat

// Tests for autopay-bot-fallback-float-waste: in the no-plan fallback the
// auto-pay adapter must take the manual policy only when the tap gate is
// tapping toward a card it can actually cast now -- instant speed, or the
// seat's own main phase with an empty stack; not a C8-dead counter; and
// affordable (printed cost plus the CR 903.8 command tax within the mana the
// seat can produce this turn). Otherwise it answers with the plan path
// (land drop / ability / pass) and no activations, so it never floats mana
// toward a card it cannot cast and lets the pool empty at step end.
//
// Board.CastableNow / AnyCastableNow (botpolicy/tap.go) are the one home of
// the predicate; these tests pin the adapter's two branches over synthetic
// Boards.

import (
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAutoPayFallbackDoesNotFloatForUncastable is the brief's headline test.
//
//   - (1) the opponent's main phase, no plans, one sorcery-speed creature in
//     hand and untapped Forests offered as activate: the tap gate wants the
//     creature but it is not a cast the window can make, so the adapter must
//     answer pass (not tap).
//   - (2) the seat's own main phase 1, empty stack, no plans, an X spell the
//     offered lands can pay: that IS a castable-now card, so the adapter must
//     take its manual answer -- an activate -- and not fall through to pass.
//   - (3) the affordability half: a 7-drop on five lands is not castable now,
//     so the no-plan window must NOT tap toward it.
func TestAutoPayFallbackDoesNotFloatForUncastable(t *testing.T) {
	// (1) Opponent's main phase, sorcery-speed creature.
	cards1 := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(),
		50: {Creature: true, Power: 3, Toughness: 3, CMC: 3, Castable: true, ManaCost: "2 G"},
	}
	brd1 := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: false, Cards: cards1}
	d1 := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "activate", Obj: 2},
			{Index: 2, Kind: "pass"},
		}}
	// Precondition: the tap gate really does intend the sorcery-speed
	// creature (otherwise the assertion below is vacuous).
	if id, ok := brd1.TapIntent(&d1); !ok || id != 50 {
		t.Fatalf("case 1 precondition: TapIntent = (%d, %v), want the creature 50", id, ok)
	}
	if _, ok := brd1.CastableNow(d1.Player, &d1); ok {
		t.Fatalf("case 1 precondition: CastableNow must be false for a sorcery in the opponent's main phase")
	}
	in1 := NewBot(19).EnableAutoPayMana().decide(brd1, &d1)
	if in1.Payment != nil || len(in1.Choices) != 1 || d1.Options[in1.Choices[0]].Kind != "pass" {
		t.Fatalf("case 1: intent = %+v, want pass (do not tap toward a sorcery in the opponent's main phase)", in1)
	}

	// (2) Own main phase 1, empty stack, an X spell the lands can pay.
	cards2 := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(), 3: forestCard(),
		50: {Creature: true, Power: 3, Toughness: 3, CMC: 3, Castable: true, ManaCost: "X G G"},
	}
	brd2 := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true, Cards: cards2}
	d2 := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "activate", Obj: 2},
			{Index: 2, Kind: "activate", Obj: 3}, {Index: 3, Kind: "pass"},
		}}
	// Precondition: the X spell really is the tap gate's intent and really is
	// castable now; if either fails the assertion below proves nothing.
	if id, ok := brd2.TapIntent(&d2); !ok || id != 50 {
		t.Fatalf("case 2 precondition: TapIntent = (%d, %v), want the X spell 50", id, ok)
	}
	if id, ok := brd2.CastableNow(d2.Player, &d2); !ok || id != 50 {
		t.Fatalf("case 2 precondition: CastableNow = (%d, %v), want the X spell 50 castable now", id, ok)
	}
	in2 := NewBot(19).EnableAutoPayMana().decide(brd2, &d2)
	if in2.Payment != nil || len(in2.Choices) != 1 || d2.Options[in2.Choices[0]].Kind != "activate" {
		t.Fatalf("case 2: intent = %+v, want an activate (the X spell is castable now)", in2)
	}

	// (3) Own main phase 1, empty stack, a 7-drop on five lands.
	cards3 := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(), 3: forestCard(), 4: forestCard(), 5: forestCard(),
		50: {Creature: true, Power: 7, Toughness: 7, CMC: 7, Castable: true, ManaCost: "6 G"},
	}
	brd3 := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true, Cards: cards3}
	d3 := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3, 4, 5} {
		d3.Options = append(d3.Options, decision.Option{Index: len(d3.Options), Kind: "activate", Obj: id})
	}
	d3.Options = append(d3.Options, decision.Option{Index: len(d3.Options), Kind: "pass"})
	// Precondition: the 7-drop is the tap gate's intent but is NOT affordable
	// with five lands (producible 5 < cost 7).
	if id, ok := brd3.TapIntent(&d3); !ok || id != 50 {
		t.Fatalf("case 3 precondition: TapIntent = (%d, %v), want the 7-drop 50", id, ok)
	}
	if _, ok := brd3.CastableNow(d3.Player, &d3); ok {
		t.Fatalf("case 3 precondition: CastableNow must be false for a 7-drop on five lands")
	}
	in3 := NewBot(19).EnableAutoPayMana().decide(brd3, &d3)
	if in3.Payment != nil || len(in3.Choices) != 1 || d3.Options[in3.Choices[0]].Kind != "pass" {
		t.Fatalf("case 3: intent = %+v, want pass (do not tap toward a 7-drop on five lands)", in3)
	}
}

// TestAutoPayFallbackKeepsPlanForUncastableIntent is the plan-bearing control:
// when a plan EXISTS and the tap gate's intent (an unaffordable 7-drop) is not
// castable now, the adapter must keep the plan path -- the affordability gate
// applies to the plan-bearing window too, not only the no-plan one.
func TestAutoPayFallbackKeepsPlanForUncastableIntent(t *testing.T) {
	cardsMap := map[state.ObjID]botpolicy.Card{
		1: forestCard(), 2: forestCard(), 3: forestCard(), 4: forestCard(), 5: forestCard(),
		42: {Creature: true, Power: 2, Toughness: 2, CMC: 2, Castable: true, ManaCost: "1 G"},
		50: {Creature: true, Power: 7, Toughness: 7, CMC: 7, Castable: true, ManaCost: "6 G"},
	}
	brd := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true, Cards: cardsMap}
	d := decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for _, id := range []state.ObjID{1, 2, 3, 4, 5} {
		d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "activate", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	d.PaymentActions = []decision.PaymentAction{paymentAction("pay", 42)}

	// Precondition: the 7-drop really is the tap gate's intent and really is
	// not affordable; otherwise the assertion below proves nothing.
	if id, ok := brd.TapIntent(&d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the 7-drop 50", id, ok)
	}
	if _, ok := brd.CastableNow(d.Player, &d); ok {
		t.Fatalf("precondition: CastableNow must be false for a 7-drop on five lands")
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment == nil || in.Payment.ActionID != "pay" {
		t.Fatalf("intent = %+v, want the offered plan kept (an unaffordable tap intent must not divert to manual)", in)
	}
}

// TestAutoPayFallbackTapsIndeterminateUrzaLands is the measured tron
// regression (bench seed 5500000, tron:uw-tempo, 46.7% -> 20.0%): the
// seat's own main phase 1, empty stack, two untapped Urza lands and
// Expedition Map in hand. The V1 planner offers NO payment action (it
// withholds a source whose Amount$ is an SVar, Count$UrzaLands), so this is
// the no-plan window; the Urza lands project an Indeterminate production with
// zero guaranteed counts, and reading that as "produces nothing" made the
// castable-now gate refuse Map and answer pass, turn after turn. An unknown
// amount must not refute affordability: the adapter takes the manual answer
// and taps a land toward Map.
func TestAutoPayFallbackTapsIndeterminateUrzaLands(t *testing.T) {
	urza := func() botpolicy.Card {
		var p cards.ManaProduction
		p.Indeterminate = true
		return botpolicy.Card{OnBattlefield: true, Produces: p}
	}
	brd := botpolicy.Board{IsMain: true, FirstMain: true, MyTurn: true,
		Cards: map[state.ObjID]botpolicy.Card{
			1: urza(), 2: urza(),
			50: {CMC: 1, Castable: true, ManaCost: "1"},
		}}
	d := decision.Decision{Seq: 47, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "activate", Obj: 2},
			{Index: 2, Kind: "pass"},
		}}
	// Precondition: the manual policy alone taps here (so a pass below is the
	// adapter's doing, not the policy's), and no payment plan is offered.
	if len(d.PaymentActions) != 0 {
		t.Fatalf("precondition: the window must carry no payment action")
	}
	manual := NewBot(19).decide(brd, &d)
	if len(manual.Choices) != 1 || d.Options[manual.Choices[0]].Kind != "activate" {
		t.Fatalf("precondition: the manual bot answers %+v, want an activate", manual)
	}

	in := NewBot(19).EnableAutoPayMana().decide(brd, &d)
	if in.Payment != nil || len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Fatalf("intent = %+v, want an activate: two untapped Urza lands can pay Expedition Map", in)
	}
}
