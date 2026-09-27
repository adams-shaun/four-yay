package botpolicy

// Tests for autopay-bot-fallback-float-waste's "castable now" predicate:
// Board.CastableNow (the tap gate's best intent) and Board.AnyCastableNow
// (any unpayable castable card), the one home the auto-pay adapter's
// no-plan/plan-bearing fallback branches read. Each test asserts the
// precondition the predicate turns on, so a vacuous Board setup fails loudly.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func nowForest() Card {
	var p cards.ManaProduction
	p.Colour[state.MG] = 1
	return Card{OnBattlefield: true, Basic: true, Produces: p}
}

func nowIsland() Card {
	var p cards.ManaProduction
	p.Colour[state.MU] = 1
	return Card{OnBattlefield: true, Basic: true, Produces: p}
}

// TestCastableNowCommandTaxAffordability pins the affordability read: the cost
// is the printed cost PLUS the CR 903.8 command tax, so a commander that would
// be affordable tax-free is not castable now once the tax pushes it past the
// seat's producible mana.
func TestCastableNowCommandTaxAffordability(t *testing.T) {
	brd := Board{IsMain: true, FirstMain: true, MyTurn: true,
		Cards: map[state.ObjID]Card{
			1: nowForest(), 2: nowForest(), 3: nowForest(), 4: nowForest(), 5: nowForest(),
			50: {Creature: true, Power: 5, Toughness: 5, CMC: 5, Castable: true, ManaCost: "4 G"},
		},
		Commanders: map[state.ObjID]Commander{50: {InCommandZone: true, Casts: 1}},
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "pass"}}}

	// Precondition: five untapped Forests produce 5, and the commander's cost
	// plus its {2} tax (printed 5 + 2 = 7) exceeds that.
	if got := brd.producibleMana(); got != 5 {
		t.Fatalf("precondition: producibleMana = %d, want 5", got)
	}
	if got := brd.castCost(50, brd.Cards[50]); got != 7 {
		t.Fatalf("precondition: castCost = %d, want 7 (5 printed + 2 command tax)", got)
	}
	if _, ok := brd.CastableNow(d.Player, d); ok {
		t.Fatalf("CastableNow = true for a commander whose cost plus tax exceeds producible mana")
	}

	// With the tax paid off (no prior command-zone cast) the printed 5 fits
	// in the producible 5, so the same board is castable now.
	brd.Commanders[50] = Commander{InCommandZone: true, Casts: 0}
	if _, ok := brd.CastableNow(d.Player, d); !ok {
		t.Fatalf("CastableNow = false for a tax-free commander whose printed cost fits producible mana")
	}
}

// TestCastableNowAnyBroadReading pins the branch distinction: the tap gate's
// single best intent can be a C8-dead counter while a different instant in the
// same hand is castable now. CastableNow (the narrow, plan-bearing reading)
// says no; AnyCastableNow (the no-plan reading) says yes.
func TestCastableNowAnyBroadReading(t *testing.T) {
	brd := Board{IsMain: true, FirstMain: true, MyTurn: true,
		Cards: map[state.ObjID]Card{
			1: nowIsland(), 2: nowIsland(),
			// 50: the higher-CMC intent, a dead counter (own spell on the
			// stack, so no foreign spell).
			50: {CMC: 4, Castable: true, ManaCost: "3 U", Counter: true, InstantSpeed: true},
			// 60: a cheaper instant the manual policy could pay by hand.
			60: {CMC: 1, Castable: true, ManaCost: "U", InstantSpeed: true},
		},
		Stack: []StackEntry{{ID: 90, Controller: 0, IsSpell: true, CMC: 4}},
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "pass"}}}

	// Preconditions: the intent really is the dead counter (50 outscores 60),
	// and 60 really is a live instant in hand.
	if id, ok := brd.TapIntent(d); !ok || id != 50 {
		t.Fatalf("precondition: TapIntent = (%d, %v), want the dead counter 50", id, ok)
	}
	if !brd.CounterIsDead(d.Player, 50) {
		t.Fatalf("precondition: CounterIsDead(50) = false, want it dead")
	}
	if !brd.Cards[60].InstantSpeed {
		t.Fatalf("precondition: card 60 must be instant speed")
	}

	if _, ok := brd.CastableNow(d.Player, d); ok {
		t.Fatalf("CastableNow = true, want false: the best intent is a C8-dead counter")
	}
	if !brd.AnyCastableNow(d.Player, d) {
		t.Fatalf("AnyCastableNow = false, want true: instant 60 is a live cast in this window")
	}
}

// TestAnyCastableNowSkipsUnproducibleColour pins that the broad scan keeps the
// tap gate's satisfiability filter: a green card whose pip no offered source
// can produce is not a card this window's taps can enable, so it must not
// justify the manual fallback (that would float an island toward a colour it
// cannot supply).
func TestAnyCastableNowSkipsUnproducibleColour(t *testing.T) {
	brd := Board{IsMain: true, FirstMain: true, MyTurn: true,
		Cards: map[state.ObjID]Card{
			1: nowIsland(), 2: nowIsland(),
			50: {Creature: true, Power: 2, Toughness: 2, CMC: 2, Castable: true, ManaCost: "1 G"},
		},
	}
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority,
		Options: []decision.Option{{Index: 0, Kind: "activate", Obj: 1}, {Index: 1, Kind: "activate", Obj: 2}, {Index: 2, Kind: "pass"}}}

	// Precondition: the card really is unpayable, and the offered sources
	// really can supply no green (only islands).
	if brd.poolPays(50, brd.Cards[50]) {
		t.Fatalf("precondition: card 50 must be unpayable")
	}
	offered := brd.offeredColours(d)
	if offered[state.MG] {
		t.Fatalf("precondition: no offered source produces green")
	}
	if !offered[state.MU] {
		t.Fatalf("precondition: the islands must produce blue")
	}

	if brd.AnyCastableNow(d.Player, d) {
		t.Fatalf("AnyCastableNow = true for a green card no offered source can pay")
	}
}
