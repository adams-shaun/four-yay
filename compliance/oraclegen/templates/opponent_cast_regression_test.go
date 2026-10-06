package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestOracleYouControlBecomesTargetStillUsesOwnCast(t *testing.T) {
	reg := loadGenRegistry(t)
	name := "Illuminator Virtuoso"
	it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.becomes-target")
	var cast *oraclegen.Step
	for i := range it.Steps {
		if it.Steps[i].Op == "cast" {
			cast = &it.Steps[i]
			break
		}
	}
	if cast == nil || cast.Seat != 0 || cast.Card != "p0:Giant Growth" || len(cast.Targets) != 1 || cast.Targets[0] != "p0:"+name {
		t.Fatalf("precondition: you-control target trigger probe = %+v, want p0 Giant Growth targeting p0:%s", cast, name)
	}
	if !containsString(it.Scenario.Setup["p0"].Hand, "Giant Growth") {
		t.Fatalf("precondition: p0 does not hold Giant Growth: %+v", it.Scenario.Setup["p0"].Hand)
	}
	if !triggerShownOnStack(t, reg, it.Scenario, name) {
		t.Fatalf("p0's own spell did not put %s's ability on the stack", name)
	}
}
