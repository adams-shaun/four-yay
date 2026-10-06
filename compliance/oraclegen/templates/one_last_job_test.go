package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestOneLastJobGeneratesMountMode pins the first mode the generator can now
// serve: a Mount from the caster's graveyard, returned by One Last Job.
func TestOneLastJobGeneratesMountMode(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "One Last Job")
	if skip != nil {
		t.Fatalf("One Last Job: %s", skip.Reason)
	}
	if !containsString(it.Setup["p0"].Graveyard, "Alacrian Jaguar") {
		t.Fatalf("precondition: One Last Job's Mount target is not in p0's graveyard: %+v", it.Setup["p0"].Graveyard)
	}
	cast := castStep(t, it, "One Last Job")
	var picked []string
	for _, answer := range cast.Answers {
		if answer.Kind == "modes" {
			picked = answer.Pick
		}
	}
	if len(picked) != 1 || !strings.Contains(picked[0], "Return target Mount or Vehicle") {
		t.Fatalf("One Last Job mode pick = %v, want Mount or Vehicle mode", picked)
	}
	if len(cast.Targets) != 1 || cast.Targets[0] != "p0:Alacrian Jaguar" {
		t.Fatalf("One Last Job Mount-mode targets = %v, want [p0:Alacrian Jaguar]", cast.Targets)
	}
	if !faceHasType(t, reg, "Alacrian Jaguar", "Mount") {
		t.Fatal("precondition: Alacrian Jaguar is not a Mount in the corpus")
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("One Last Job Mount scenario did not play through cleanly: %v", res.Fails)
	}
}
