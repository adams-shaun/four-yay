package templates

import "testing"

// TestGeneratedMandatoryCastTargetsSurvive checks both directions of the
// target contract. A clean replay alone cannot detect a missing mandatory
// target when a cast was reversed or the fixture happened to have no targets.
// It checks the representative required cards; the exhaustive pinned-corpus
// sweep is the TestTargetAuditChunkN family (target_audit_test.go).
func TestGeneratedMandatoryCastTargetsSurvive(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := representativeTargetCards
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
		mandatory += checkCastTargetsSurvive(t, c)
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
