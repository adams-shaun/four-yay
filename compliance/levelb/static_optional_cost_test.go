package levelb

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

// TestStaticOptionalCostClassification pins which OptionalCost statics are
// served (the self-spell shape with a payable cost part) and which stay a
// visible gap (another cost part, an extra param, a non-self static).
func TestStaticOptionalCostClassification(t *testing.T) {
	self := func(cost string, extra map[string]string) cards.Static {
		p := map[string]string{"EffectZone": "All", "ValidCard": "Card.Self", "ValidSA": "Spell", "Cost": cost, "Description": "As an additional cost"}
		for k, v := range extra {
			p[k] = v
		}
		return stat("OptionalCost", p)
	}
	spell := func(st cards.Static) *cards.Card {
		return cardOf(&cards.Face{Types: []string{"Sorcery"}, Statics: []cards.Static{st}})
	}
	served := []Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.optional-cost"}}
	for _, tc := range []struct {
		name string
		st   cards.Static
		want []Requirement
	}{
		{"collect evidence", self("CollectEvidence<8>", nil), served},
		{"blight", self("Blight<2>", nil), served},
		{"behold a Dragon", self("Behold<1/Dragon>", nil), served},
		{"waterbend", self("Waterbend<4>", nil), served},
		{"compound cost is classified (the template skips it by name)", self("ChooseCreatureType<1> Behold<2/Creature.ChosenType>", nil), served},
		{"reveal stays a gap", self("Reveal<1/Dragon>", nil),
			[]Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.gap:OptionalCost", Gap: "static mode OptionalCost"}}},
		{"an extra param stays a gap", self("Blight<1>", map[string]string{"Condition": "PlayerTurn"}),
			[]Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.gap:OptionalCost", Gap: "static mode OptionalCost"}}},
		{"a non-self static stays a gap", self("Blight<1>", map[string]string{"ValidCard": "Card.YouCtrl"}),
			[]Requirement{{Key: "static#0.0", Family: "static", Face: 0, Slot: "0", Sub: "static.gap:OptionalCost", Gap: "static mode OptionalCost"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Requirements(spell(tc.st))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Requirements = %+v, want %+v", got, tc.want)
			}
		})
	}
}
