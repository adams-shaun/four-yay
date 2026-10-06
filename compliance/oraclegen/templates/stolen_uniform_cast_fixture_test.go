package templates

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestStolenUniformCastFixture pins the generated two-target cast fixture for
// Stolen Uniform, whose card script includes a delayed Unattach ability.
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
	if !strings.Contains(face.SVars["TrigUnattach"], "DB$ Unattach") || !strings.Contains(face.SVars["DBDelayTrig"], "DB$ DelayedTrigger") {
		t.Fatalf("precondition: Stolen Uniform must carry its delayed Unattach ability: TrigUnattach=%q DBDelayTrig=%q", face.SVars["TrigUnattach"], face.SVars["DBDelayTrig"])
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
	res, ok := oraclegen.PlaysThrough(reg, item.Scenario)
	if !ok {
		t.Fatalf("generated Stolen Uniform scenario does not play through gorge: %+v", item.Scenario)
	}
	// The fixture must really exercise the attach: after the resolve step the
	// stolen Equipment is on p0's creature (the root Pump's RememberObjects$
	// hands that creature to the chain's Defined$ Remembered Attach).
	if len(res.Snapshots) == 0 {
		t.Fatalf("generated Stolen Uniform scenario recorded no snapshots")
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if !strings.Contains(final.Checkpoint, "resolve") {
		t.Fatalf("last snapshot %q is not the post-resolution checkpoint", final.Checkpoint)
	}
	attached := false
	for _, p := range final.Permanents {
		if p.Name == "Accorder's Shield" {
			if p.Controller != 0 || p.AttachedTo != "p0:Grizzly Bears" {
				t.Fatalf("after resolve: Accorder's Shield = %+v, want controlled by p0 and attached to p0:Grizzly Bears\n%s",
					p, strings.Join(res.Transcript, "\n"))
			}
			attached = true
		}
	}
	if !attached {
		t.Fatalf("after resolve: Accorder's Shield is not on the battlefield: %+v", final.Permanents)
	}
}
