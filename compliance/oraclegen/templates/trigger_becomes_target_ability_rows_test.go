package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// The ValidSource$ Ability shape (Loki, God of Mischief) generates from the
// targeted-ability cause, plays through gorge, shows Loki's own trigger on the
// stack, and the trigger's consequence is visible: Loki's TrigDraw leaves p0's
// final hand one card larger than the scenario's first snapshot.
func TestBecomesTargetAbilityRowGenerates(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Loki, God of Mischief", "trigger#0.0", "trigger.becomes-target-ability")
	if res, ok := oraclegen.PlaysThrough(reg, it.Scenario); !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	if !g17SlotOnStack(t, reg, it.Scenario, "Loki, God of Mischief", "0") {
		t.Fatalf("no snapshot shows trigger 0 on the stack sourced by Loki, God of Mischief")
	}
	res := runSteps(t, reg, it.Scenario, it.Scenario.Steps)
	if len(res.Snapshots) < 2 {
		t.Fatalf("precondition: %d snapshots, want at least a setup and a final one", len(res.Snapshots))
	}
	first, last := res.Snapshots[0], res.Snapshots[len(res.Snapshots)-1]
	if got, want := len(last.Players[0].Hand), len(first.Players[0].Hand)+1; got != want {
		t.Fatalf("Loki's TrigDraw did not draw: final p0 hand = %d, want %d (setup %d)", got, want, len(first.Players[0].Hand))
	}
}
