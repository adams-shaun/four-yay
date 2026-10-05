package templates

import (
	"strings"
	"testing"
)

func TestFieryAnnihilationAttachedEquipmentFixture(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Fiery Annihilation")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	cast := castStep(t, it, "Fiery Annihilation")
	if len(cast.Targets) != 1 || !strings.Contains(cast.Targets[0], "Grizzly Bears") {
		t.Fatalf("cast targets %q, want its mandatory creature target", cast.Targets)
	}
	pre := it.Scenario.Steps[0]
	if pre.Op != "attach" || pre.AttachedTo != cast.Targets[0] {
		t.Fatalf("attach prelude = %+v, want Equipment attached to %s", pre, cast.Targets[0])
	}
	seat := it.Scenario.Setup["p1"]
	equipment := strings.TrimPrefix(pre.Card, "p1:")
	if !containsBattlefieldCard(seat.Battlefield, equipment) {
		t.Fatalf("Equipment %q absent from p1 battlefield %v", pre.Card, seat.Battlefield)
	}
	faceHasType(t, reg, equipment, "Equipment")
	foundOptionalAnswer := false
	for _, step := range it.Scenario.Steps {
		for _, answer := range step.Answers {
			for _, pick := range answer.Pick {
				if pick == equipment {
					foundOptionalAnswer = true
				}
			}
		}
	}
	if !foundOptionalAnswer {
		t.Fatalf("optional Equipment %q is not selected by a generated answer", equipment)
	}
	bearer := strings.TrimPrefix(cast.Targets[0], "p1:")
	if !containsBattlefieldCard(seat.Battlefield, bearer) {
		t.Fatalf("bearer %q absent from p1 battlefield %v", bearer, seat.Battlefield)
	}
}

func containsBattlefieldCard(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
