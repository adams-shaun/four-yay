package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestCostFixtureFrogPresent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	face := &cards.Face{ManaCost: "2 G", Statics: []cards.Static{{
		Mode: "ReduceCost", Params: map[string]string{
			"ValidCard": "Card.Self", "Type": "Spell", "Amount": "1", "IsPresent": "Frog.YouCtrl",
		},
	}}}
	probes, gap, handled := parameterCostProbes(reg, face, "Frog condition probe", 0)
	if !handled || len(probes) == 0 {
		t.Fatalf("precondition: Frog IsPresent shape not handled (gap %q)", gap)
	}
	probe := probes[0]
	if len(probe.battlefield) == 0 {
		t.Fatalf("Frog condition board is empty: %+v", probe)
	}
	for _, name := range probe.battlefield {
		card, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("Frog condition fixture %q does not resolve", name)
		}
		isFrog := false
		for _, typ := range card.Faces[0].Types {
			isFrog = isFrog || typ == "Frog"
		}
		if !isFrog {
			t.Fatalf("Frog condition fixture %q has types %v", name, card.Faces[0].Types)
		}
	}
}

// TestCostFixtureCardsGenerate: the IsPresent / count fixtures come from the
// shared condition helpers, not from a table of card names, so the test pins
// what the fixture must do rather than which card it is: the board is
// non-empty, the reduced-price cast replays, and it fails with the static gone.
func TestCostFixtureCardsGenerate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Pearl of Wisdom", "Rime Chill", "Wildvine Pummeler"} {
		t.Run(name, func(t *testing.T) {
			item := staticCostItem(t, reg, name)
			if len(item.Scenario.Setup["p0"].Battlefield) == 0 {
				t.Fatalf("precondition: no fixture on the cost fixture board: %+v", item.Scenario.Setup["p0"])
			}
			if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
				t.Fatal("reduced-price fixture scenario does not play through")
			}
			if _, ok := oraclegen.PlaysThrough(withoutStatics(reg, name), item.Scenario); ok {
				t.Fatal("reduced-price cast still plays through without the reduction")
			}
		})
	}
}
