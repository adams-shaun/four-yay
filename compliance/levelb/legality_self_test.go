package levelb

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestSelfLegalityStaticShapes pins which self-referential combat-legality
// statics are served (cli-20261006T132127Z-b96121b3) and that every
// conditional or filtered sibling stays the named "legality static" gap.
func TestSelfLegalityStaticShapes(t *testing.T) {
	creature := func(st cards.Static) (string, string) {
		f := &cards.Face{Types: []string{"Creature"}, Statics: []cards.Static{st}}
		return classifyStatic(f, &f.Statics[0])
	}
	for _, tc := range []struct {
		name string
		st   cards.Static
		sub  string
	}{
		{"CantBlock self", stat("CantBlock", map[string]string{"Mode": "CantBlock", "ValidCard": "Card.Self", "Description": "d"}), "static.cant-block-self"},
		{"CantBlock Creature.Self", stat("CantBlock", map[string]string{"ValidCard": "Creature.Self"}), "static.cant-block-self"},
		{"CantBlockBy self", stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Self"}), "static.cant-block-by-self"},
		{"MinMaxBlocker Min 3", stat("MinMaxBlocker", map[string]string{"ValidCard": "Creature.Self", "Min": "3"}), "static.min-blockers"},
		// Gaps: every one of these changes WHEN or FOR WHOM the restriction holds.
		{"CantBlock conditional", stat("CantBlock", map[string]string{"ValidCard": "Card.Self", "Condition": "Threshold"}), ""},
		{"CantBlock IsPresent", stat("CantBlock", map[string]string{"ValidCard": "Card.Self", "IsPresent": "Land.YouCtrl+untapped"}), ""},
		{"CantBlock other creatures", stat("CantBlock", map[string]string{"ValidCard": "Creature.EnchantedBy"}), ""},
		{"CantBlockBy blocker filter", stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Self", "ValidBlocker": "Creature.powerLE2"}), "static.cant-block-by-blocker-filter"},
		{"CantBlockBy controller-scoped blocker filter", stat("CantBlockBy", map[string]string{"ValidAttacker": "Creature.Self", "ValidBlocker": "Creature.YouCtrl"}), ""},
		{"CantBlockBy CheckSVar", stat("CantBlockBy", map[string]string{"ValidAttacker": "Card.Self", "CheckSVar": "X"}), ""},
		{"MinMaxBlocker Max", stat("MinMaxBlocker", map[string]string{"ValidCard": "Card.Self", "Max": "1"}), ""},
		{"MinMaxBlocker All", stat("MinMaxBlocker", map[string]string{"ValidCard": "Card.Self", "Min": "All"}), ""},
		{"MinMaxBlocker bound 1", stat("MinMaxBlocker", map[string]string{"ValidCard": "Card.Self", "Min": "1"}), ""},
		{"MinMaxBlocker gated", stat("MinMaxBlocker", map[string]string{"ValidCard": "Card.Self", "Min": "3", "IsPresent": "Card.Other"}), ""},
	} {
		sub, gap := creature(tc.st)
		if tc.sub != "" && (sub != tc.sub || gap != "") {
			t.Errorf("%s: classified %q gap %q, want %q served", tc.name, sub, gap, tc.sub)
		}
		if tc.sub == "" && (sub != "static.combat" || gap != "legality static") {
			t.Errorf("%s: classified %q gap %q, want the legality static gap", tc.name, sub, gap)
		}
	}
	// A non-creature has no block to observe.
	f := &cards.Face{Types: []string{"Artifact"}, Statics: []cards.Static{stat("CantBlock", map[string]string{"ValidCard": "Card.Self"})}}
	if _, gap := classifyStatic(f, &f.Statics[0]); gap != "legality static" {
		t.Errorf("non-creature CantBlock gap %q, want the legality static gap", gap)
	}
}
