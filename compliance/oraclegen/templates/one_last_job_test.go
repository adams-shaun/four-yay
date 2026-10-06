package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
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
	resolveStep := -1
	for i, step := range it.Steps {
		if step.Op == "resolve" {
			resolveStep = i
			break
		}
	}
	if resolveStep < 0 || resolveStep >= len(it.XAnswers) {
		t.Fatalf("One Last Job has no XMage answers for its resolve step: step=%d answers=%v", resolveStep, it.XAnswers)
	}
	foundAttachAnswer := false
	for _, answer := range it.XAnswers[resolveStep] {
		if answer.Seat == 0 && answer.Kind == "target" && answer.Value == "p0:Llanowar Elves" {
			foundAttachAnswer = true
		}
	}
	if !foundAttachAnswer {
		t.Fatalf("One Last Job resolve answers %v omit the scripted attachment creature", it.XAnswers[resolveStep])
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("One Last Job AuraEquipment scenario did not resolve cleanly: ok=%v failures=%v", ok, res.Fails)
	}

	// The returned card must actually leave the graveyard onto the
	// battlefield and attach to an intended p0 creature (Oracle: "return
	// target Aura or Equipment card from your graveyard to the battlefield
	// attached to a creature you control"; CR 303.4, CR 701.3). Completion
	// alone would pass with an inert return-and-attach effect.
	final := res.Snapshots[len(res.Snapshots)-1]
	target := cast.Targets[0]
	var returned *rules.OracleSnapPerm
	for i := range final.Permanents {
		if final.Permanents[i].Ref == target {
			returned = &final.Permanents[i]
		}
	}
	if returned == nil {
		t.Fatalf("One Last Job target %s is not on the battlefield: permanents=%v",
			target, permanentRefs(final.Permanents))
	}
	if returned.Controller != 0 {
		t.Fatalf("One Last Job returned %s under controller %d, want p0", target, returned.Controller)
	}
	bearer := returned.AttachedTo
	if bearer == "" {
		t.Fatalf("One Last Job returned %s unattached; want it attached to a creature you control", target)
	}
	if !strings.HasPrefix(bearer, "p0:") {
		t.Fatalf("One Last Job attached %s to %q; want a p0 creature", target, bearer)
	}
	var attachedTo *rules.OracleSnapPerm
	for i := range final.Permanents {
		if final.Permanents[i].Ref == bearer {
			attachedTo = &final.Permanents[i]
		}
	}
	if attachedTo == nil {
		t.Fatalf("One Last Job attach bearer %q is not a battlefield permanent: permanents=%v",
			bearer, permanentRefs(final.Permanents))
	}
	if attachedTo.Controller != 0 || !containsString(attachedTo.Types, "Creature") {
		t.Fatalf("One Last Job attached %s to %q (controller %d, types %v); want a p0 creature",
			target, bearer, attachedTo.Controller, attachedTo.Types)
	}
}

// permanentRefs is the battlefield ref list, for a failure message.
func permanentRefs(perms []rules.OracleSnapPerm) []string {
	refs := make([]string, 0, len(perms))
	for _, p := range perms {
		refs = append(refs, p.Ref)
	}
	return refs
}
