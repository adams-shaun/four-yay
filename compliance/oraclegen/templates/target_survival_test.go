package templates

import (
	"reflect"
	"testing"
)

// TestGeneratedMandatoryCastTargetsSurvive checks both directions of the
// target contract. A clean replay alone cannot detect a missing mandatory
// target when a cast was reversed or the fixture happened to have no targets.
// By default it checks the representative required cards; set
// GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1 to run the exhaustive pinned-corpus sweep.
func TestGeneratedMandatoryCastTargetsSurvive(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetAuditCards(t, reg)
	mandatory, replayed := 0, 0
	// These two corpus cards require a cast-time creature target. A broken
	// rewrite can silently turn a generated card into a skip, so checking
	// only scenarios that survived generation would be vacuous.
	required := map[string]bool{"Conduct Electricity": false, "Repulsive Mutation": false}
	for _, c := range targetAuditCases(t, reg, carriers) {
		name := c.name
		if c.skip != nil {
			if _, ok := required[name]; ok {
				t.Errorf("mandatory-target card %s was skipped: %s", name, c.skip.Reason)
			}
			continue
		}
		if _, ok := required[name]; ok {
			required[name] = true
		}
		replayed++
		if c.err != nil || len(c.fails) != 0 {
			t.Errorf("%s: replay err=%v fails=%v", name, c.err, c.fails)
			continue
		}
		for i, st := range c.steps {
			if st.op != "cast" {
				continue
			}
			var picks []string
			for _, d := range c.decisions {
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
			if len(picks) == 0 && len(st.targets) == 0 {
				continue
			}
			if !reflect.DeepEqual(st.targets, picks) {
				t.Errorf("%s cast step %d: scenario targets %v, gorge picked %v", name, i, st.targets, picks)
			}
		}
	}
	t.Logf("target survival: %d carriers, %d scenarios replayed, %d mandatory target decisions", len(carriers), replayed, mandatory)
	if mandatory == 0 {
		t.Fatal("precondition: corpus generated no mandatory target decisions")
	}
	for name, generated := range required {
		if !generated {
			t.Errorf("precondition: mandatory-target carrier %s was not generated", name)
		}
	}
}
