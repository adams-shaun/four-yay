package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/pay"
)

// TestAvailableManaAbilitiesForWindowFiltersInPlace pins the heap-object POC
// cut that compacts the payment-window set in place instead of copying it into
// a second list. The priority window keeps every ability; the payment window
// keeps exactly the subset without an InstantSpeed$ restriction, in the same
// order and membership as a reference filter.
func TestAvailableManaAbilitiesForWindowFiltersInPlace(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	id := onBoard(t, e, 0, "Name:LEDland\nManaCost:no cost\nTypes:Land\n"+
		"A:AB$ Mana | Cost$ T | Produced$ W\n"+
		"A:AB$ Mana | Cost$ T | Produced$ G | InstantSpeed$ True\nOracle:x\n")
	all := e.availableManaAbilities(0, id)
	pri := e.availableManaAbilitiesForWindow(0, id, true)
	if !reflect.DeepEqual(pri, all) {
		t.Fatalf("priority window = %v, want all %v", pri, all)
	}
	instant := 0
	for _, ma := range all {
		if pay.InstantSpeedOnly(ma) {
			instant++
		}
	}
	if instant == 0 {
		t.Fatal("fixture has no InstantSpeed$ True ability to filter")
	}
	want := make([]*cards.SA, 0, len(all)-instant)
	for _, ma := range all {
		if !pay.InstantSpeedOnly(ma) {
			want = append(want, ma)
		}
	}
	got := e.availableManaAbilitiesForWindow(0, id, false)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payment window = %v, want %v", got, want)
	}
	if len(got) != len(all)-instant {
		t.Fatalf("payment window kept %d, want %d of %d", len(got), len(all)-instant, len(all))
	}
}
