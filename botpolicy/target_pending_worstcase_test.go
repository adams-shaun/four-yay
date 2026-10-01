package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestTargetPendingGenericPaymentDrainsTightReserveColour covers a generic
// pending payment when the reserve has multiple coloured pips. The allocation
// must account for slack, not merely prefer the colour with the largest pip
// count: spending a tight colour can destroy the reserve.
func TestTargetPendingGenericPaymentDrainsTightReserveColour(t *testing.T) {
	green := pendingGreen()
	blue := pendingGreen()
	blue.Colour[state.MG] = 0
	blue.Colour[state.MU] = 1
	b := Board{
		Cards: TableOf(map[state.ObjID]Card{
			1: {OnBattlefield: true, Basic: true, Produces: green},
			2: {OnBattlefield: true, Basic: true, Produces: green},
			3: {OnBattlefield: true, Basic: true, Produces: green},
			4: {OnBattlefield: true, Basic: true, Produces: green},
			5: {OnBattlefield: true, Basic: true, Produces: green},
			6: {OnBattlefield: true, Basic: true, Produces: blue},
			7: {OnBattlefield: true, Basic: true, Produces: blue},
			8: {Castable: true, InstantSpeed: true, ManaCost: "G G U", CMC: 3},
		}),
		Stack: []StackEntry{{ID: 50, IsSpell: true, ManaCost: "2", CMC: 2}},
	}
	if !b.hasSpareMana() {
		t.Fatal("precondition: reserve should be spare before pending payment")
	}
	if got := colourPips(b.Cards.Get(8).ManaCost); got[state.MG] != 2 || got[state.MU] != 1 {
		t.Fatalf("reserve precondition pips = %v, want G:2 U:1", got)
	}
	if got, ok := b.pendingSpendWorstCase(state.Mana{state.MG: 5, state.MU: 2}, "2", []Card{b.Cards.Get(8)}); !ok || got[state.MU] != 0 {
		t.Fatalf("worst-case deduction = %v, ok=%v; want generic units to exhaust tight U reserve", got, ok)
	}
	if b.hasSpareManaAfter("2") {
		t.Fatal("generic pending payment can consume the tight reserve colour; must not claim spare")
	}
}
