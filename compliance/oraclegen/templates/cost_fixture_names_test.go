package templates

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestCostFixtureNamesResolve(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if len(costPresentFixtures) == 0 || len(costColourFixtures) != 3 {
		t.Fatal("precondition: cost fixture collections are empty or incomplete")
	}
	keys := make([]string, 0, len(costPresentFixtures))
	for typ := range costPresentFixtures {
		keys = append(keys, typ)
	}
	sort.Strings(keys)
	for _, typ := range keys {
		name := costPresentFixtures[typ]
		card, ok := reg.Lookup(name)
		if !ok || len(card.Faces) == 0 {
			t.Errorf("%s fixture %q does not resolve in corpus", typ, name)
			continue
		}
		matched := false
		for _, cardType := range card.Faces[0].Types {
			matched = matched || strings.EqualFold(cardType, typ)
		}
		if !matched {
			t.Errorf("%s fixture %q has types %v", typ, name, card.Faces[0].Types)
		}
	}
	for _, name := range costColourFixtures {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("colour fixture %q does not resolve in corpus", name)
		}
	}
}

func TestCostFixtureFrogPresent(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	face := &cards.Face{ManaCost: "2 G", Statics: []cards.Static{{
		Mode: "ReduceCost", Params: map[string]string{
			"ValidCard": "Card.Self", "Type": "Spell", "Amount": "1", "IsPresent": "Frog.YouCtrl",
		},
	}}}
	probe, ok := parameterCostProbe(reg, face, "Frog condition probe", 0)
	if !ok {
		t.Fatal("precondition: Frog IsPresent shape not handled")
	}
	if !containsString(probe.battlefield, "Yargle, Glutton of Urborg") {
		t.Fatalf("Frog condition board = %v", probe.battlefield)
	}
	for _, name := range probe.battlefield {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("Frog condition fixture %q does not resolve", name)
		}
	}
}

func TestCostFixtureCardsGenerate(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ name, fixture string }{
		{"Pearl of Wisdom", "Thieving Otter"},
		{"Rime Chill", "Thieving Otter"},
		{"Wildvine Pummeler", "Thieving Otter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := staticCostItem(t, reg, tc.name)
			if !containsString(item.Scenario.Setup["p0"].Battlefield, tc.fixture) {
				t.Fatalf("precondition: %s absent from cost fixture board: %+v", tc.fixture, item.Scenario.Setup["p0"])
			}
			if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
				t.Fatal("reduced-price fixture scenario does not play through")
			}
			if _, ok := oraclegen.PlaysThrough(withoutStatics(reg, tc.name), item.Scenario); ok {
				t.Fatal("reduced-price cast still plays through without the reduction")
			}
		})
	}
}
