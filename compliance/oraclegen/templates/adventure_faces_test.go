package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

func TestAdventureSpellFacesGenerateFixtures(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name      string
		parent    string
		wantSlots []string
	}{
		{name: "Burglar's Plot", parent: "Bilbo, Luckwearer", wantSlots: []string{"Permanent.nonLand", "Permanent.nonLand"}},
		{name: "Gone Fishing", parent: "Lake-town Mariners", wantSlots: []string{"Creature.YouCtrl,Land.YouCtrl", "Creature.YouCtrl,Land.YouCtrl"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) < 2 {
				t.Fatalf("precondition: %s Adventure card absent or missing faces", tc.name)
			}
			var spellFace, parentFace bool
			for _, face := range card.Faces {
				switch face.Name {
				case tc.name:
					spellFace = true
					if got := oraclegen.TargetSlots(face); len(got) != len(tc.wantSlots) {
						t.Fatalf("precondition: %s spell face target slots = %v, want %v", tc.name, got, tc.wantSlots)
					} else {
						for i := range got {
							if got[i] != tc.wantSlots[i] {
								t.Fatalf("precondition: %s spell face target slots = %v, want %v", tc.name, got, tc.wantSlots)
							}
						}
					}
				case tc.parent:
					parentFace = true
				}
			}
			if !spellFace || !parentFace {
				t.Fatalf("precondition: %s parent/spell faces present = %v/%v", tc.name, parentFace, spellFace)
			}

			item, skip := Generate(reg, tc.name)
			if skip != nil {
				t.Fatalf("Generate skipped %s: %s", tc.name, skip.Reason)
			}
			var cast *oraclegen.Step
			for i := range item.Scenario.Steps {
				step := &item.Scenario.Steps[i]
				if step.Op == "cast" && step.Card == "p0:"+tc.name {
					cast = step
					break
				}
			}
			if cast == nil {
				t.Fatalf("generated scenario has no cast of requested Adventure face %q: %+v", tc.name, item.Scenario.Steps)
			}
			if len(cast.Targets) != len(tc.wantSlots) {
				t.Fatalf("%s cast targets = %v, want %d required targets (%v)", tc.name, cast.Targets, len(tc.wantSlots), tc.wantSlots)
			}
			for i, target := range cast.Targets {
				if target == "" || target == "p0:"+tc.name {
					t.Fatalf("%s target %d is not a fixture object: %q", tc.name, i, target)
				}
			}
			result, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(result.Fails) != 0 {
				t.Fatalf("%s replay err=%v fails=%v", tc.name, err, result.Fails)
			}
		})
	}
}
