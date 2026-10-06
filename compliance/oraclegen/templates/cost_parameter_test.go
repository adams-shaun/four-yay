package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestCostStaticParameterProfiles(t *testing.T) {
	reg := loadGenRegistry(t)
	cases := []struct {
		name, mana, extra string
	}{
		{"Polliwallop", "G", ""},
		{"Dire Downdraft", "CCU", ""},
		{"Eddymurk Crab", "CCCCUU", ""},
		{"Champion of the Clachan", "CCCW", "Kithkin Greatheart"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			it := staticCostItem(t, reg, tc.name)
			cast := probeCast(t, it)
			if cast.Mana != tc.mana {
				t.Fatalf("cast mana = %q, want adjusted price %q", cast.Mana, tc.mana)
			}
			if cast.Card != "p0:"+tc.name {
				t.Fatalf("probe cast = %+v, want the card itself", cast)
			}
			if tc.name == "Polliwallop" {
				count := 0
				for _, fixture := range it.Scenario.Setup["p0"].Battlefield {
					c, ok := reg.Lookup(fixture)
					if !ok || len(c.Faces) == 0 {
						continue
					}
					for _, typ := range c.Faces[0].Types {
						if typ == "Frog" {
							count++
							break
						}
					}
				}
				if count < 3 {
					t.Fatalf("precondition: Polliwallop affinity fixtures = %d Frogs, want 3", count)
				}
			}
			if tc.name == "Dire Downdraft" {
				if len(cast.Targets) == 0 {
					t.Fatalf("precondition: Dire Downdraft cast has no target: %+v", cast)
				}
				ref := strings.TrimPrefix(cast.Targets[0], "p1:")
				tapped := false
				for _, name := range it.Scenario.Setup["p1"].Tapped {
					tapped = tapped || name == ref
				}
				if !tapped {
					t.Fatalf("precondition: target %q is not tapped", cast.Targets[0])
				}
				if _, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok {
					t.Fatalf("precondition: tapped-target reduced-cost scenario does not play through: %+v", it.Steps)
				}
			}
			if tc.name == "Champion of the Clachan" {
				res := runSteps(t, reg, it.Scenario, it.Steps)
				if len(res.Fails) != 0 {
					t.Fatalf("additional-cost cast failed with its fixture: %v", res.Fails)
				}
			}
			if tc.extra != "" {
				found := false
				setup := it.Scenario.Setup["p0"]
				for _, card := range setup.Hand {
					found = found || card == tc.extra
				}
				for _, card := range setup.Battlefield {
					found = found || card == tc.extra
				}
				if !found {
					t.Fatalf("precondition: additional-cost fixture %q absent from hand: %v", tc.extra, it.Scenario.Setup["p0"].Hand)
				}
			}
		})
	}
}

func TestOwnRaiseCostIsClassifiedAsServed(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Champion of the Clachan", "Officious Interrogation", "Dragon's Prey"} {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("precondition: %s missing from corpus", name)
		}
		found := false
		for _, req := range levelb.Requirements(c) {
			if req.Key == "static#0.0" {
				found = true
				if req.Sub != "static.cost" || req.Gap != "" {
					t.Fatalf("own RaiseCost requirement for %s = %+v, want served static.cost", name, req)
				}
			}
		}
		if !found {
			t.Fatalf("precondition: %s has no static#0.0", name)
		}
	}
}
