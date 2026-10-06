package rules_test

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestOracleAttachPrelude(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	item, skip := templates.Generate(reg, "Fiery Annihilation")
	if skip != nil {
		t.Fatal(skip.Reason)
	}
	var castTargets []string
	var equipmentRef string
	var attachStep int = -1
	for i, step := range item.Scenario.Steps {
		if step.Op == "attach" {
			attachStep = i
			equipmentRef = step.Card
		}
		if step.Op == "cast" && step.Card == "p0:Fiery Annihilation" {
			castTargets = step.Targets
		}
	}
	if attachStep < 0 || len(castTargets) != 1 || equipmentRef == "" {
		t.Fatalf("steps missing attach or mandatory creature target: attach=%d targets=%q", attachStep, castTargets)
	}
	if attachStep >= 0 && item.Scenario.Steps[attachStep].AttachedTo != castTargets[0] {
		t.Fatalf("attach bearer %q, want parent creature target %q", item.Scenario.Steps[attachStep].AttachedTo, castTargets[0])
	}
	if equipmentRef == castTargets[0] {
		t.Fatalf("Equipment ref %q must differ from its creature bearer", equipmentRef)
	}
	result, err := rules.RunOracleScenarioJSON(reg, item.Raw())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Fails) != 0 {
		t.Fatalf("scenario failures: %v\n%s", result.Fails, strings.Join(result.Transcript, "\n"))
	}
	var attached, exiled bool
	for _, snap := range result.Snapshots {
		if snap.Checkpoint == "step 0 (attach)" {
			equip, ok := oracleAttachPerm(snap, equipmentRef)
			if !ok || equip.AttachedTo != castTargets[0] {
				t.Fatalf("attached Equipment snapshot = %+v (%v), want bearer %q", equip, ok, castTargets[0])
			}
			if !containsString(equip.Types, "Equipment") {
				t.Fatalf("attached target lacks printed Equipment type: %+v", equip.Types)
			}
			bearer, ok := oracleAttachPerm(snap, castTargets[0])
			if !ok || !containsString(bearer.Types, "Creature") {
				t.Fatalf("parent target is not a battlefield creature: %+v (%v)", bearer, ok)
			}
			attached = true
		}
	}
	if !attached {
		t.Fatal("attach operation did not produce an observable attachment snapshot")
	}
	final := result.Snapshots[len(result.Snapshots)-1]
	for _, card := range final.Players[1].Exile {
		if card == strings.TrimPrefix(equipmentRef, "p1:") {
			exiled = true
		}
	}
	if !exiled {
		t.Fatalf("target Equipment %q not exiled by Fiery Annihilation: p1 exile=%q", equipmentRef, final.Players[1].Exile)
	}
}

func oracleAttachPerm(s rules.OracleSnapshot, ref string) (rules.OracleSnapPerm, bool) {
	for _, p := range s.Permanents {
		if p.Ref == ref {
			return p, true
		}
	}
	return rules.OracleSnapPerm{}, false
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
