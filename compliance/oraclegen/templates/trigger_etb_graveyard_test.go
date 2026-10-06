package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// All nine graveyard sources in the brief must be tried from their trigger
// zone. Darklight Phoenix's phase condition is now met by the count-aware
// turn-history preludes; Persistent Marshstalker still keeps its graveyard-zone
// skip (trigger did not fire from the graveyard).
func TestTriggerETBProbeGraveyardSources(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, tc := range []struct{ name, sub, skip string }{
		{"Bloodghast", "trigger.etb-other", ""},
		{"Gate Colossus", "trigger.etb-other", ""},
		{"Redtooth Vanguard", "trigger.etb-other", ""},
		{"Fear of Infinity", "trigger.etb-other", ""},
		{"Shambling Cie'th", "trigger.spell-cast", ""},
		{"Wolfbat", "trigger.drawn", ""},
		{"Furious Forebear", "trigger.dies-other", ""},
		{"Darklight Phoenix", "trigger.phase", ""},
		{"Persistent Marshstalker", "trigger.attacks", "trigger did not fire from the graveyard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := reg.Lookup(tc.name)
			if !ok {
				t.Fatalf("precondition: %s missing from corpus", tc.name)
			}
			var req levelb.Requirement
			found := false
			for _, r := range levelb.Requirements(c) {
				if r.Key == "trigger#0.0" {
					req, found = r, true
				}
			}
			if !found || req.Sub != tc.sub || req.CoveredByA {
				t.Fatalf("precondition: requirement found=%v %+v", found, req)
			}
			f := c.Faces[0]
			if !strings.Contains(f.Triggers[0].ParamStr(cards.PKTriggerZones), "Graveyard") {
				t.Fatal("precondition: trigger does not name Graveyard")
			}
			causes, why := triggerRecipe(reg, f, tc.name, &f.Triggers[0], req.Sub)
			if why != "" || len(causes) == 0 {
				t.Fatalf("precondition: no recipe: %s", why)
			}
			for _, cause := range causes {
				sc := triggerScenario(f, tc.name, cause, req, cause.steps, &oraclegen.Fixture{})
				assertTriggerProbeInGraveyard(t, sc, tc.name)
			}
			it, skip := GenerateB(reg, tc.name, req)
			if tc.skip != "" {
				if skip == nil || skip.Reason != tc.skip {
					t.Fatalf("skip=%v, want %q", skip, tc.skip)
				}
				return
			}
			if skip != nil {
				t.Fatalf("unexpected skip: %s", skip.Reason)
			}
			assertTriggerProbeInGraveyard(t, it.Scenario, tc.name)
			if !triggerProbeSlotOnStack(t, reg, it.Scenario, tc.name, req.Slot) {
				t.Fatalf("graveyard trigger %s never reaches the stack", req.Key)
			}
		})
	}
}

func assertTriggerProbeInGraveyard(t *testing.T, sc oraclegen.Scenario, name string) {
	t.Helper()
	p0 := sc.Setup["p0"]
	if !containsString(p0.Graveyard, name) || containsString(p0.Battlefield, name) {
		t.Fatalf("%s must be in graveyard only: graveyard=%v battlefield=%v", name, p0.Graveyard, p0.Battlefield)
	}
}

// Inspect the requested slot independently of the generator's acceptance
// helper: an unrelated trigger on the same source cannot prove this row.
func triggerProbeSlotOnStack(t *testing.T, reg *cards.Registry, sc oraclegen.Scenario, name, slot string) bool {
	t.Helper()
	var cause []oraclegen.Step
	for _, st := range sc.Steps {
		if st.Op != "resolve" {
			cause = append(cause, st)
		}
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	for _, steps := range [][]oraclegen.Step{cause, append(append([]oraclegen.Step(nil), cause...), passes...)} {
		res := runSteps(t, reg, sc, steps)
		for _, snap := range res.Snapshots {
			for _, entry := range snap.Stack {
				if entry.Kind == "ability" && entry.Source == "p0:"+name && entry.Trigger == slot {
					return true
				}
			}
		}
	}
	return false
}
