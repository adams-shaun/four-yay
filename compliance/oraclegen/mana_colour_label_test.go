package oraclegen

import "testing"

// A costed mana ability's colour option carries its cost ("Pay 1: Add W"), and
// is a colour dialog, not a hybrid/phyrexian payment half.
func TestManaColourLabelDropsTheCostPrefix(t *testing.T) {
	for label, want := range map[string]string{
		"Add W": "White", "Pay 1: Add U": "Blue", "Pay 2 life: Add B": "Black", "Pay 1 life: Add G": "Green",
	} {
		if got, ok := manaColourLabel(label); !ok || got != want {
			t.Errorf("manaColourLabel(%q) = %q, %v; want %q", label, got, ok, want)
		}
	}
	for _, label := range []string{"Add C", "Pay 1: Add any color", "Pay 1: Add", "Pay {W/U} with W", "Add WU"} {
		if got, ok := manaColourLabel(label); ok {
			t.Errorf("manaColourLabel(%q) = %q, want no colour", label, got)
		}
	}
	if payment([]string{"Pay 1: Add W"}) {
		t.Error(`payment("Pay 1: Add W") = true: a colour pick is not a payment half`)
	}
	if !payment([]string{"Pay 2 life"}) {
		t.Error(`payment("Pay 2 life") = false: a phyrexian half is still a payment`)
	}
}
