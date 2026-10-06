package templates_test

import (
	"strings"
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
					if step.Op == "cast" && step.Card == "Shock" && hasCard(step.Targets, "p1") {
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

func TestStaticIsPresentGraveyardHasNamedSkip(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Basking Capybara")
	if !ok {
		t.Fatal("precondition: Basking Capybara is in the corpus")
	}
	for _, req := range levelb.Requirements(c) {
		if req.Sub != "static.continuous" {
			continue
		}
		_, skip := templates.GenerateB(reg, "Basking Capybara", req)
		if skip == nil || !strings.Contains(skip.Reason, "counts permanent cards in a graveyard") {
			t.Fatalf("Basking Capybara %s skip = %v, want the named graveyard-filter limitation", req.Key, skip)
		}
		return
	}
	t.Fatal("precondition: Basking Capybara has a static.continuous requirement")
}

func hasCard(xs []string, card string) bool {
	for _, x := range xs {
		if x == card {
			return true
		}
	}
	return false
}
