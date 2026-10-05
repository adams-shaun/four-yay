package templates

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestStolenUniformCastFixture pins the generated two-target cast fixture for
// Stolen Uniform, whose delayed Unattach ability is compiled during replay.
func TestStolenUniformCastFixture(t *testing.T) {
	reg := oracleHarnessCorpus(t)
	card, ok := reg.Lookup("Stolen Uniform")
	if !ok || len(card.Faces) == 0 {
		t.Fatalf("precondition: Stolen Uniform must exist in the corpus; found=%t faces=%d", ok, len(card.Faces))
	}
	face := card.Faces[0]
	if !strings.Contains(face.Oracle, "Choose target creature you control and target Equipment") {
		t.Fatalf("precondition: Stolen Uniform oracle text lacks its two-target instruction: %q", face.Oracle)
	}
	wantSlots := []string{"Creature.YouCtrl", "Equipment"}
	if slots := oraclegen.TargetSlots(face); !reflect.DeepEqual(slots, wantSlots) {
		t.Fatalf("precondition: Stolen Uniform target slots = %v, want %v", slots, wantSlots)
	}

	item, skip := Generate(reg, "Stolen Uniform")
	if skip != nil {
		t.Fatalf("Stolen Uniform unexpectedly skipped: %s", skip.Reason)
	}
	var cast *oraclegen.Step
	for i := range item.Steps {
		if item.Steps[i].Op == "cast" && item.Steps[i].Card == "p0:Stolen Uniform" {
			cast = &item.Steps[i]
			break
		}
	}
	if cast == nil {
		t.Fatalf("generated scenario has no cast step for Stolen Uniform: %+v", item.Steps)
	}
	wantTargets := []string{"p0:Grizzly Bears", "p1:Accorder's Shield"}
	if !reflect.DeepEqual(cast.Targets, wantTargets) {
		t.Fatalf("Stolen Uniform cast targets = %v, want %v", cast.Targets, wantTargets)
	}
	if _, ok := oraclegen.PlaysThrough(reg, item.Scenario); !ok {
		t.Fatalf("generated Stolen Uniform scenario does not play through gorge: %+v", item.Scenario)
	}
}
