package templates

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// TestGeneratedMandatoryCastTargetsSurvive checks both directions of the
// target contract. A clean replay alone cannot detect a missing mandatory
// target when a cast was reversed or the fixture happened to have no targets.
func TestGeneratedMandatoryCastTargetsSurvive(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetCarriers(t, reg)
	mandatory := 0
	// These two corpus cards require a cast-time creature target. A broken
	// rewrite can silently turn a generated card into a skip, so checking
	// only scenarios that survived generation would be vacuous.
	required := map[string]bool{"Conduct Electricity": false, "Repulsive Mutation": false}
	for _, name := range carriers {
		it, skip := Generate(reg, name)
		if skip != nil {
			if _, ok := required[name]; ok {
				t.Errorf("mandatory-target card %s was skipped: %s", name, skip.Reason)
			}
			continue
		}
		if _, ok := required[name]; ok {
			required[name] = true
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil || len(res.Fails) != 0 {
			t.Errorf("%s: replay err=%v fails=%v", name, err, res.Fails)
			continue
		}
		for i, st := range it.Scenario.Steps {
			if st.Op != "cast" {
				continue
			}
			var picks []string
			for _, d := range res.Decisions {
				if d.Step != i || d.Via != "target" {
					continue
				}
				if d.Min > 0 {
					mandatory++
					if len(d.PickRefs) < d.Min {
						t.Errorf("%s: mandatory target decision %+v was not answered", name, d)
					}
				}
				picks = append(picks, d.PickRefs...)
			}
			if len(picks) == 0 && len(st.Targets) == 0 {
				continue
			}
			if !reflect.DeepEqual(st.Targets, picks) {
				t.Errorf("%s cast step %d: scenario targets %v, gorge picked %v", name, i, st.Targets, picks)
			}
		}
	}
	if mandatory == 0 {
		t.Fatal("precondition: corpus generated no mandatory target decisions")
	}
	for name, generated := range required {
		if !generated {
			t.Errorf("precondition: mandatory-target carrier %s was not generated", name)
		}
	}
}
