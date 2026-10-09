package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestTriggerSpellCastProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, probe string
		steps       int
	}{
		{"Cosmogrand Zenith", "Shock", 2},
		{"Gnarlback Rhino", growthProbe, 1},
		{"Spinerock Tyrant", "Shock", 1},
		{"Helga, Skittish Seer", "Air Elemental", 1},
		{"Archmage of Runes", "", 0},
		{"Balmor, Battlemage Captain", "", 0},
		{"Guttersnipe", "", 0},
		{"Rite of the Dragoncaller", "", 0},
		{"Thousand-Year Storm", "", 0},
		{"Danitha, Spear of Agony", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 || len(card.Faces[0].Triggers) == 0 {
				t.Fatalf("precondition: %s trigger is unavailable", tc.name)
			}
			causes, why := triggerRecipe(reg, card.Faces[0], tc.name, &card.Faces[0].Triggers[0], "trigger.spell-cast")
			if why != "" || len(causes) == 0 {
				t.Fatalf("precondition: spell-cast handler did not produce causes: %q (%d)", why, len(causes))
			}
			found := false
			for _, cause := range causes {
				if len(cause.steps) != tc.steps || len(cause.hand) != tc.steps {
					continue
				}
				matches := true
				for i, step := range cause.steps {
					want := "p0:" + tc.probe
					if i == 1 {
						want += "#2"
					}
					matches = matches && step.Op == "cast" && step.Card == want
				}
				if matches {
					found = true
					break
				}
			}
			if tc.probe != "" && !found {
				t.Fatalf("expected %d cast step(s) using %s among %d causes", tc.steps, tc.probe, len(causes))
			}
			var req levelb.Requirement
			for _, candidate := range levelb.Requirements(card) {
				if candidate.Key == "trigger#0.0" {
					req = candidate
					break
				}
			}
			if req.Key == "" || req.Sub != "trigger.spell-cast" {
				t.Fatalf("precondition: %s trigger requirement missing or classified %q", tc.name, req.Sub)
			}
			item, skip := GenerateB(reg, tc.name, req)
			if skip != nil {
				t.Fatalf("GenerateB did not serve the firing trigger: %s", skip.Reason)
			}
			if item.Card != tc.name || item.Template != req.Key {
				t.Fatalf("generated wrong requirement: %s %s", item.Card, item.Template)
			}
		})
	}
}

func TestSpellCastTypeAlternatives(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	instant, ok := reg.Lookup("Shock")
	if !ok || len(instant.Faces) == 0 {
		t.Fatal("precondition: Shock is unavailable")
	}
	if !spellProbeMatchesType(instant.Faces[0], "Instant.singleTarget,Sorcery.singleTarget") {
		t.Fatal("Instant alternative did not satisfy comma-separated type filter")
	}
}
