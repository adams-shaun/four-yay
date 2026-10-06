package templates

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestAttackTriggerCrewActivationExportsCrewChoice(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card, key, ability string }{
		{"Boosted Sloop", "trigger#0.0", "Crew 1"},
		{"Great Gilded Boat", "trigger#0.0", "Crew 2"},
		{"Haunted Hellride", "trigger#0.0", "Crew 1"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.card, tc.key, "trigger.attacks")
			if len(it.Scenario.Steps) < 3 || it.Scenario.Steps[0].Op != "activate" {
				t.Fatalf("precondition: trigger scenario does not activate before attacking: %+v", it.Scenario.Steps)
			}
			if len(it.XAbility) == 0 || it.XAbility[0] != tc.ability {
				t.Fatalf("XMage ability prefix = %v, want %q", it.XAbility, tc.ability)
			}
			if len(it.XAnswers) <= 0 || len(it.XAnswers[0]) != 1 || it.XAnswers[0][0].Kind != "choice" || it.XAnswers[0][0].Value != "Colossal Dreadmaw" {
				t.Fatalf("Crew tap choice not exported for activation step: %+v", it.XAnswers)
			}
		})
	}
}

func TestGeneratedTriggerSetupDoesNotResolveEntryDiscard(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := triggerRequirement(t, reg, "Kefka, Court Mage", "trigger#0.1", "trigger.attacks")
	p0 := it.Scenario.Setup["p0"]
	if !containsString(p0.Hand, "Wastes") {
		t.Fatalf("precondition: generated Kefka fixture lacks Wastes discard filler: %v", p0.Hand)
	}
	b, err := json.Marshal(it.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 || len(res.Snapshots[0].Players) == 0 {
		t.Fatalf("setup snapshot missing: %+v", res.Snapshots)
	}
	if got := res.Snapshots[0].Players[0].Hand; len(got) != 1 || got[0] != "Wastes" {
		t.Fatalf("generated fixture setup hand = %v, want [Wastes] before scenario steps", got)
	}
}
