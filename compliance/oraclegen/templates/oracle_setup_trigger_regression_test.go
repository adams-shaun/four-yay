package templates

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestGeneratedSetupKeepsCounterAddedTrigger(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := triggerRequirement(t, reg, "Inspired Tethermage", "trigger#0.0", "trigger.loyalty-activated")
	p0 := it.Scenario.Setup["p0"]
	if !containsString(p0.Battlefield, "Inspired Tethermage") || !containsString(p0.Battlefield, "Ajani Goldmane") {
		t.Fatalf("precondition: setup battlefield = %v, want Inspired Tethermage and Ajani Goldmane", p0.Battlefield)
	}
	b, err := json.Marshal(it.Scenario)
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules.RunOracleScenarioJSON(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Snapshots) == 0 {
		t.Fatal("setup snapshot missing")
	}
	for _, p := range res.Snapshots[0].Permanents {
		if p.Name == "Inspired Tethermage" {
			if p.PT != "4/3" || p.Counters["P1P1"] != 1 {
				t.Fatalf("setup Tethermage = %s counters=%v, want 4/3 with one P1P1 counter", p.PT, p.Counters)
			}
			return
		}
	}
	t.Fatal("precondition: Inspired Tethermage missing from setup snapshot")
}
