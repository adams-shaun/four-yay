package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

func TestOneLastJobAuraEquipmentModeFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "One Last Job")
	if skip != nil {
		t.Fatalf("One Last Job: %s", skip.Reason)
	}
	cast := castStep(t, it, "One Last Job")
	foundMode := false
	for _, answer := range cast.Answers {
		if answer.Kind == "modes" {
			for _, mode := range answer.Pick {
				if strings.Contains(mode, "Aura or Equipment") {
					foundMode = true
				}
			}
		}
	}
	if !foundMode {
		t.Fatalf("One Last Job mode answer %v does not include AuraEquipment", cast.Answers)
	}
	if len(cast.Targets) == 0 {
		t.Fatalf("One Last Job AuraEquipment mode has no graveyard target")
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("One Last Job AuraEquipment scenario did not resolve cleanly: ok=%v failures=%v", ok, res.Fails)
	}
}
