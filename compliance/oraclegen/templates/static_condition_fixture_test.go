package templates_test

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func staticRequirement(t *testing.T, name string) (oraclegen.Item, bool) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s is in the corpus", name)
	}
	for _, req := range levelb.Requirements(c) {
		if req.Sub == "static.continuous" && req.Gap == "" {
			item, skip := templates.GenerateB(reg, name, req)
			if skip != nil {
				return oraclegen.Item{}, false
			}
			return item, true
		}
	}
	t.Fatalf("precondition: %s has a static.continuous requirement", name)
	return oraclegen.Item{}, false
}

func TestStaticConditionalFixtureExamples(t *testing.T) {
	cases := []struct {
		name string
		want func(map[string]oraclegen.Seat) bool
	}{
		{"Boneclub Berserker", func(seats map[string]oraclegen.Seat) bool {
			return hasCard(seats["p0"].Battlefield, "Swab Goblin")
		}},
		{"Nightwhorl Hermit", func(seats map[string]oraclegen.Seat) bool {
			return len(seats["p0"].Graveyard) >= 7
		}},
		{"Omenport Vigilante", func(seats map[string]oraclegen.Seat) bool {
			return len(seats["p0"].Hand) > 0
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item, served := staticRequirement(t, tc.name)
			if !served {
				t.Fatalf("%s static was not observed", tc.name)
			}
			if !tc.want(item.Setup) {
				t.Fatalf("%s fixture does not meet its expected setup: %+v", tc.name, item.Setup)
			}
			if !hasCard(item.Setup["p0"].Battlefield, "Grizzly Bears") || !hasCard(item.Setup["p1"].Battlefield, "Grizzly Bears") {
				t.Fatalf("precondition: p0 and p1 both have the observation probe")
			}
			if tc.name == "Omenport Vigilante" {
				foundCrime := false
				for _, step := range item.Scenario.Steps {
					if step.Op == "cast" && step.Card == "p0:Shock" && hasCard(step.Targets, "p1") {
						foundCrime = true
					}
				}
				if !foundCrime {
					t.Fatalf("crime fixture does not target p1 with Shock: %+v", item.Scenario.Steps)
				}
			}
		})
	}
}

func TestStaticSelfConditionTurnHistory(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Brightspear Zealot")
	if !ok {
		t.Fatal("precondition: Brightspear Zealot is in the corpus")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Key != "static#0.0" || req.Sub != "static.continuous" {
			continue
		}
		item, skip := templates.GenerateB(reg, "Brightspear Zealot", req)
		if skip != nil {
			t.Fatalf("Brightspear Zealot: %s", skip.Reason)
		}
		if len(item.Scenario.Steps) < 4 {
			t.Fatalf("precondition: two prelude casts precede the tested card: %+v", item.Scenario.Steps)
		}
		return
	}
	t.Fatal("precondition: Brightspear Zealot static#0.0 requirement exists")
}

func TestStaticIsPresentGraveyardScenarios(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, key string
		minimum   int
	}{
		{"Basking Capybara", "static#0.0", 4},
		{"Echo of Dusk", "static#0.0", 4},
		{"Didact Echo", "static#0.0", 4},
		{"Frilled Cave-Wurm", "static#0.0", 4},
		{"Akawalli, the Seething Tower", "static#0.0", 4},
		{"Akawalli, the Seething Tower", "static#0.1", 8},
	} {
		t.Run(tc.card+"/"+tc.key, func(t *testing.T) {
			card, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("precondition: %s is in corpus", tc.card)
			}
			var req *levelb.Requirement
			for i := range levelb.Requirements(card) {
				r := levelb.Requirements(card)[i]
				if r.Key == tc.key && r.Sub == "static.continuous" {
					req = &r
					break
				}
			}
			if req == nil {
				t.Fatalf("precondition: %s %s requirement exists", tc.card, tc.key)
			}
			item, skip := templates.GenerateB(reg, tc.card, *req)
			if skip != nil {
				t.Fatalf("unexpected skip: %s", skip.Reason)
			}
			if len(item.Setup["p0"].Graveyard) < tc.minimum {
				t.Fatalf("graveyard fixture has %d cards, want >=%d: %+v", len(item.Setup["p0"].Graveyard), tc.minimum, item.Setup)
			}
		})
	}
}

func hasCard(xs []string, card string) bool {
	for _, x := range xs {
		if x == card {
			return true
		}
	}
	return false
}
