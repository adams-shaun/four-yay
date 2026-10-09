package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestTriggerSiblingOrderProbe pins a target trigger queued with a sibling:
// Lunar Convocation's second end-step ability requires both life gain and loss.
func TestTriggerSiblingOrderProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name, slot, stackSlot = "Lunar Convocation", "trigger#0.1", "1"
	it := triggerItem(t, reg, name, slot)
	castsDistinct(t, it.Scenario, 2, "Angel's Mercy", "Shock")
	if !inZone(it.Scenario.Setup["p0"].Battlefield, name) {
		t.Fatalf("precondition: %s is not on p0's battlefield: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}

	steps := append([]oraclegen.Step(nil), drawCauseSteps(it.Scenario)...)
	steps = append(steps, oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass_to", Decision: "priority"})

	// Prove the cause really establishes both sides of the intervening-if:
	// Angel's Mercy raises p0 from 20, then Shock lowers that raised total.
	cause := runSteps(t, reg, it.Scenario, drawCauseSteps(it.Scenario))
	if len(cause.Fails) != 0 || len(cause.Snapshots) == 0 {
		t.Fatalf("history cause fails: %v", cause.Fails)
	}
	gained, lost := false, false
	var previous int32
	for i, snap := range cause.Snapshots {
		life := snap.Players[0].Life
		if i == 0 {
			previous = life
			continue
		}
		if life > previous {
			gained = true
		}
		if gained && life < previous {
			lost = true
		}
		previous = life
	}
	if !gained || !lost {
		t.Fatalf("precondition: p0 life history did not gain then lose life (gain=%t loss=%t): %v", gained, lost, lifeTotals(cause.Snapshots))
	}

	// Before answering the order ask the requested trigger is not yet on the
	// stack. The final pass_to answers it and exposes both exact trigger slots.
	before := runSteps(t, reg, it.Scenario, steps[:len(steps)-1])
	if len(before.Fails) != 0 || len(before.Snapshots) == 0 {
		t.Fatalf("pre-answer trigger-order replay fails: %v", before.Fails)
	}
	orderStep := -1
	for _, d := range before.Decisions {
		if d.GorgeKind == "trigger_order" || d.Kind == "order" {
			orderStep = d.Step
			break
		}
	}
	if orderStep < 0 || orderStep > len(steps) {
		t.Fatalf("precondition: trigger-order ask has no valid scenario step: %+v", before.Decisions)
	}
	preAsk := runSteps(t, reg, it.Scenario, steps[:orderStep])
	if len(preAsk.Fails) != 0 || len(preAsk.Snapshots) == 0 {
		t.Fatalf("pre-order replay fails: %v", preAsk.Fails)
	}
	if hasTriggerSlot(preAsk.Snapshots[len(preAsk.Snapshots)-1], name, stackSlot) {
		t.Fatalf("precondition: requested %s was already on stack before answering sibling order: %+v", slot, preAsk.Snapshots[len(preAsk.Snapshots)-1].Stack)
	}

	res := runSteps(t, reg, it.Scenario, steps)
	if len(res.Fails) != 0 || len(res.Snapshots) == 0 {
		t.Fatalf("trigger-order checkpoint fails: %v", res.Fails)
	}
	order := 0
	for _, d := range res.Decisions {
		if d.GorgeKind == "trigger_order" || d.Kind == "order" {
			order++
			if d.Options != 2 || len(d.Picks) != 2 {
				t.Fatalf("trigger-order decision options=%d picks=%v, want both siblings", d.Options, d.Picks)
			}
		}
	}
	if order != 1 {
		t.Fatalf("trigger-order decisions = %d, want one for both siblings: %+v", order, res.Decisions)
	}
	if !abilityOnStack(res.Snapshots, stackSourceWants(reg, name, wantsFace(t, reg, name)), stackSlot) {
		t.Fatalf("checkpoint must expose requested slot %s after sibling order ask, got snapshots=%+v decisions=%+v", slot, res.Snapshots, res.Decisions)
	}
}

func hasTriggerSlot(snap rules.OracleSnapshot, name, slot string) bool {
	for _, e := range snap.Stack {
		if e.Kind == "ability" && e.Trigger == slot && strings.Contains(strings.ToLower(e.Source), strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func lifeTotals(snaps []rules.OracleSnapshot) []int32 {
	out := make([]int32, 0, len(snaps))
	for _, snap := range snaps {
		out = append(out, snap.Players[0].Life)
	}
	return out
}
