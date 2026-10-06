package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

func TestTriggerPhaseRecipes(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct {
		name, key, wantStep, wantActive string
	}{
		{"Kamahl, Heart of Krosa", "trigger#0.0", "begin-combat", ""},
		{"Killer Service", "trigger#0.1", "end", ""},
		{"Karma", "trigger#0.0", "upkeep", "p1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := triggerRequirement(t, reg, tc.name, tc.key, "trigger.phase")
			var checkpoint []oraclegen.Step
			found := false
			for _, name := range it.Scenario.Setup["p0"].Battlefield {
				found = found || name == tc.name
			}
			if !found {
				t.Fatalf("precondition: trigger source %s is not on p0's battlefield", tc.name)
			}
			for i, st := range it.Scenario.Steps {
				if st.Op != "pass_to" {
					t.Fatalf("step %d = %q, want pass_to phase checkpoint", i, st.Op)
				}
				if st.Step != tc.wantStep || st.Active != tc.wantActive {
					t.Fatalf("pass_to = (%q, active %q), want (%q, active %q)", st.Step, st.Active, tc.wantStep, tc.wantActive)
				}
				checkpoint = append(checkpoint, st)
				break
			}
			if len(checkpoint) == 0 || len(it.Scenario.Steps) < 2 || it.Scenario.Steps[len(it.Scenario.Steps)-1].Op != "resolve" {
				t.Fatalf("expected a phase checkpoint followed by trigger resolution: %+v", it.Scenario.Steps)
			}
			if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
				t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
			}
			res := runSteps(t, reg, it.Scenario, checkpoint)
			if !stackHasSource(res.Snapshots, tc.name) {
				t.Fatalf("phase checkpoint did not put %s's ability on the stack", tc.name)
			}
		})
	}

	want := []string{"begin-combat", "draw@p0", "draw@p1", "end", "end-combat", "main1@p0", "main2", "upkeep@p0", "upkeep@p1"}
	got := PassToSteps()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("PassToSteps() = %v, want %v", got, want)
	}
}

func stackHasSource(snaps []rules.OracleSnapshot, name string) bool {
	want := strings.ToLower(name)
	for _, snap := range snaps {
		for _, entry := range snap.Stack {
			if entry.Kind == "ability" && strings.Contains(strings.ToLower(entry.Source), want) {
				return true
			}
		}
	}
	return false
}
