package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestActivateKeywordCostTokenShapes(t *testing.T) {
	cases := []struct {
		cost, wantPool, wantGap string
	}{
		{"Waterbend<3>", "CCC", ""},
		{"Waterbend<5> T", "CCCCC", ""},
		{"Waterbend<X>", "C", ""},
		{"Waterbend<0>", "", "Waterbend<...>"},
		{"T Blight<1>", "", ""},
		{"1 R T Blight<2>", "CR", ""},
		{"Blight<3>", "", "Blight<...>"},
		{"Blight<X>", "", "Blight<...>"},
		{"T AddCounter<1/PAGE>", "", ""},
		{"2 T AddCounter<1/STUN>", "CC", ""},
		{"AddCounter<X/PAGE>", "", "AddCounter<...>"},
		{"SubCounter<1/PAGE>", "", "SubCounter<...>"},
		{"2 Forage", "CC", ""},
		{"W T Exert<1/NICKNAME>", "W", ""},
		{"T Exert<1/CARDNAME>", "", ""},
		{"Exert<2/CARDNAME>", "", "Exert<...>"},
		{"X B T PayLife<X>", "CB", ""},
		{"PayLife<X>", "", ""},
		{"XMin1 X X T", "CC", ""},
		{"4 R XMin1 ExileCtrlOrGrave<X/Dinosaur.Other>", "CCCCR", ""},
		{"8 U XMin4 ExileCtrlOrGrave<X/Permanent.Other+nonLand+hasAbility Activated/nonlands with activated abilities>", "CCCCCCCCU", ""},
		{"XMin1 ExileCtrlOrGrave<X/Planeswalker.Other>", "", "ExileCtrlOrGrave<...>"},
	}
	for _, tc := range cases {
		pool, gap := activationCostIn(tc.cost, "battlefield")
		if pool != tc.wantPool || gap != tc.wantGap {
			t.Errorf("activationCost(%q) = (%q, %q), want (%q, %q)", tc.cost, pool, gap, tc.wantPool, tc.wantGap)
		}
	}
}

// TestActivateKeywordCostScenarios serves one card per keyword cost shape and
// checks the fixture, the pool and the scripted answers the cost needs.
func TestActivateKeywordCostScenarios(t *testing.T) {
	reg := loadGenRegistry(t)

	t.Run("Flexible Waterbender", func(t *testing.T) {
		assertActivateItem(t, reg, "Flexible Waterbender", "activate#0.0", "Waterbend {3}")
		it, _ := activateRequirement(t, reg, "Flexible Waterbender", "activate#0.0")
		// Waterbend<3> is three generic mana in the pool: nothing is tapped.
		if got := it.Scenario.Steps[0].Mana; got != "CCC" {
			t.Errorf("activate pool = %q, want CCC", got)
		}
	})

	t.Run("Katara, Water Tribe's Hope", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Katara, Water Tribe's Hope", "activate#0.0")
		st := it.Scenario.Steps[0]
		if st.Mana != "C" {
			t.Errorf("Waterbend<X> pool = %q, want C (X = 1)", st.Mana)
		}
		// The waterbend tap ask precedes the X ask: decline it, then announce.
		if len(st.Answers) != 2 || len(st.Answers[0].Pick) != 0 || st.Answers[1].Pick[0] != "X = 1" {
			t.Errorf("answers = %+v, want an empty waterbend pick then X = 1", st.Answers)
		}
	})

	t.Run("Gristle Glutton", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Gristle Glutton", "activate#0.0")
		bf := it.Scenario.Setup["p0"].Battlefield
		if !containsFold(bf, blightFixture) {
			t.Fatalf("blight fixture %q absent from p0's battlefield %v", blightFixture, bf)
		}
		c, ok := reg.Lookup(blightFixture)
		if !ok || c.Faces[0].Toughness() != 3 {
			t.Fatalf("precondition: %s must be a 3-toughness creature", blightFixture)
		}
		// The blighted creature is a choice XMage asks for.
		picked := false
		for _, a := range it.XAnswers[activateStepIndex(it.Scenario.Steps)] {
			if a.Kind == "choice" && (strings.EqualFold(a.Value, blightFixture) || strings.EqualFold(a.Value, "Gristle Glutton")) {
				picked = true
			}
		}
		if !picked {
			t.Errorf("no XMage choice for the blighted creature: %+v", it.XAnswers)
		}
	})

	t.Run("Mazemind Tome", func(t *testing.T) {
		assertActivateItem(t, reg, "Mazemind Tome", "activate#0.0", "{T}, Put a page counter on {this}")
		assertActivateItem(t, reg, "Mazemind Tome", "activate#0.1", "{2}, {T}, Put a page counter on {this}")
	})

	t.Run("Saheeli's Lattice", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Saheeli's Lattice", "activate#0.0")
		if got := it.Scenario.Steps[0].Mana; got != "CCCCR" {
			t.Errorf("Craft pool = %q, want CCCCR", got)
		}
		gy := it.Scenario.Setup["p0"].Graveyard
		if !containsFold(gy, "Colossal Dreadmaw") {
			t.Fatalf("the Dinosaur material is absent from p0's graveyard %v", gy)
		}
		c, ok := reg.Lookup("Colossal Dreadmaw")
		if !ok || !oraclegen.HasType(c.Faces[0], "Dinosaur") {
			t.Fatalf("precondition: Colossal Dreadmaw must be a Dinosaur")
		}
		// X is the XMin1 floor and Craft reads it from the cost: no X answer
		// is scripted (the engine would reject it as unconsumed).
		if len(it.Scenario.Steps[0].Answers) != 0 {
			t.Errorf("Craft's exile count must not be asked: %+v", it.Scenario.Steps[0].Answers)
		}
	})

	t.Run("Krumar Initiate", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Krumar Initiate", "activate#0.0")
		st := it.Scenario.Steps[0]
		if st.Mana != "CB" || len(st.Answers) != 1 || st.Answers[0].Pick[0] != "X = 1" {
			t.Errorf("X B T PayLife<X>: pool %q answers %+v, want CB and X = 1", st.Mana, st.Answers)
		}
	})

	t.Run("Camellia, the Seedmiser", func(t *testing.T) {
		it, _ := activateRequirement(t, reg, "Camellia, the Seedmiser", "activate#0.0")
		gy := it.Scenario.Setup["p0"].Graveyard
		if len(gy) < 3 {
			t.Fatalf("forage needs three graveyard cards, got %v", gy)
		}
		c, _ := reg.Lookup("Camellia, the Seedmiser")
		if cost := c.Faces[0].Abilities[0].ParamStr(cards.PKCost); !strings.Contains(cost, "Forage") {
			t.Fatalf("precondition: ability 0 cost %q has no Forage", cost)
		}
	})

	t.Run("Basri, Tomorrow's Champion", func(t *testing.T) {
		assertActivateItem(t, reg, "Basri, Tomorrow's Champion", "activate#0.0", "{W}, {T}, Exert Basri")
	})
}
