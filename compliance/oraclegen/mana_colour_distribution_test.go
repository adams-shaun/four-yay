package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// colourPickCount is the class-level guard: a decision whose every pick names a
// single mana colour is a multi-unit colour allocation, not one colour dialog,
// so it must not emit a joined "^" colour selection (Baxter Building's "Add
// four mana in any combination of colors"). The census of level-B activate
// items is in compliance/oraclegen/templates.
func TestColourPickCount(t *testing.T) {
	multiUnit := rules.OracleDecision{
		Kind: "choose_n", Min: 4, Max: 4,
		Picks:     []string{"Pay 4: Add W", "Pay 4: Add U", "Pay 4: Add B", "Pay 4: Add R"},
		PickKinds: []string{"mana", "mana", "mana", "mana"},
	}
	if n := colourPickCount(multiUnit); n != 4 {
		t.Errorf("colourPickCount(Baxter allocation) = %d, want 4", n)
	}
	single := rules.OracleDecision{
		Kind: "choose_n", Min: 1, Max: 1,
		Picks:     []string{"Pay 1: Add W"},
		PickKinds: []string{"mana"},
	}
	if n := colourPickCount(single); n != 1 {
		t.Errorf("colourPickCount(single colour) = %d, want 1", n)
	}
	// A multi-card selection ("put two of them into your hand") is not colours.
	objects := rules.OracleDecision{
		Kind: "choose_n", Min: 0, Max: 2,
		Picks:     []string{"Wastes", "Wastes"},
		PickKinds: []string{"card", "card"},
	}
	if n := colourPickCount(objects); n != 0 {
		t.Errorf("colourPickCount(card selection) = %d, want 0", n)
	}
	// The "_pay" first ask ("Pay 4: Add four mana in any combination of
	// colors") is not one colour either; only the per-colour picks are.
	firstAsk := rules.OracleDecision{
		Kind: "choose_n", Min: 1, Max: 1,
		Picks:     []string{"Pay 4: Add four mana in any combination of colors"},
		PickKinds: []string{"mana"},
	}
	if n := colourPickCount(firstAsk); n != 0 {
		t.Errorf("colourPickCount(any-combination first ask) = %d, want 0", n)
	}
}
