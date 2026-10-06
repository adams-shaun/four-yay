package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestOracleOpponentCastProbe(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Wrecking Gecko", "Old Fat Spider"} {
		t.Run(name, func(t *testing.T) {
			it := triggerRequirement(t, reg, name, "trigger#0.0", "trigger.becomes-target")
			if len(it.Steps) < 2 {
				t.Fatalf("opponent cast steps = %+v, need p0 pass and p1 cast", it.Steps)
			}
			var cast *oraclegen.Step
			for i := range it.Steps {
				if it.Steps[i].Op == "cast" {
					cast = &it.Steps[i]
				}
			}
			if cast == nil || cast.Seat != 1 || cast.Card != "p1:Shock" || len(cast.Targets) != 1 || cast.Targets[0] != "p0:"+name {
				t.Fatalf("probe cast = %+v, want p1 Shock targeting p0:%s", cast, name)
			}
			if !containsString(it.Scenario.Setup["p1"].Hand, "Shock") {
				t.Fatalf("precondition: p1 does not hold Shock: %+v", it.Scenario.Setup["p1"].Hand)
			}
			if !triggerShownOnStack(t, reg, it.Scenario, name) {
				t.Fatalf("opponent cast did not put %s's ability on the stack", name)
			}
			result, ok := oraclegen.PlaysThrough(reg, it.Scenario)
			if !ok || len(result.Fails) != 0 {
				t.Fatalf("opponent-cast scenario does not play through: ok=%v fails=%v", ok, result.Fails)
			}
			if name == "Wrecking Gecko" {
				declined := false
				for _, answers := range it.XAnswers {
					for _, answer := range answers {
						declined = declined || (answer.Kind == "mode" && answer.Value == "[mode_skip]")
					}
				}
				if !declined {
					t.Fatalf("precondition: ward payment was not explicitly declined: answers=%+v decisions=%+v", it.XAnswers, result.Decisions)
				}
			}
		})
	}
}
