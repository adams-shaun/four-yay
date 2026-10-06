package templates

import (
	"strings"
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
			// Setup deals the PHYSICAL card under its parent name -- the name
			// both engines deal it by -- never the Adventure face's name.
			if hand := item.Scenario.Setup["p0"].Hand; len(hand) == 0 || hand[0] != tc.parent {
				t.Fatalf("%s setup hand = %v, want the parent card %q first", tc.name, hand, tc.parent)
			}
			result, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil || len(result.Fails) != 0 {
				t.Fatalf("%s replay err=%v fails=%v", tc.name, err, result.Fails)
			}
			// The runner cast the Adventure face (the engine's adventure_alt
			// offer is labelled with the face), not the creature front.
			if !strings.Contains(strings.Join(result.Transcript, "\n"), `"Cast `+tc.name+`"`) {
				t.Fatalf("%s: the Adventure face was not cast:\n%s", tc.name, strings.Join(result.Transcript, "\n"))
			}
			// CR 715.4: the resolved Adventure rests in exile (the adventure
			// zone) as its main face; it is not on the battlefield, in the
			// graveyard, or still in hand.
			final := result.Snapshots[len(result.Snapshots)-1]
			if x := final.Players[0].Exile; len(x) != 1 || x[0] != tc.parent {
				t.Fatalf("%s: p0 exile = %v, want [%s]", tc.name, x, tc.parent)
			}
			for _, h := range final.Players[0].Hand {
				if h == tc.parent {
					t.Fatalf("%s: %s still in hand", tc.name, tc.parent)
				}
			}
			for _, p := range final.Permanents {
				if p.Name == tc.parent {
					t.Fatalf("%s: the creature front %s was cast onto the battlefield", tc.name, tc.parent)
				}
			}
		})
	}
}
