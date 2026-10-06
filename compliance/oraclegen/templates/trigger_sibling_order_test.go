package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestTriggerSiblingOrderProbe pins a target trigger queued with a sibling:
// Lunar Convocation's second end-step ability requires both life gain and loss.
func TestTriggerSiblingOrderProbe(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name, slot = "Lunar Convocation", "trigger#0.1"
	it := triggerItem(t, reg, name, slot)
	castsDistinct(t, it.Scenario, 2, "Angel's Mercy", "Shock")
	if !inZone(it.Scenario.Setup["p0"].Battlefield, name) {
		t.Fatalf("precondition: %s is not on p0's battlefield: %v", name, it.Scenario.Setup["p0"].Battlefield)
	}

	steps := append([]oraclegen.Step(nil), drawCauseSteps(it.Scenario)...)
	steps = append(steps, oraclegen.Step{Op: "pass", Seat: 0}, oraclegen.Step{Op: "pass", Seat: 1},
		oraclegen.Step{Op: "pass_to", Decision: "priority"})
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
	at := res.Snapshots[len(res.Snapshots)-1]
	seenTarget := false
	for _, e := range at.Stack {
		if e.Kind != "ability" || !strings.Contains(strings.ToLower(e.Source), strings.ToLower(name)) {
			continue
		}
		seenTarget = seenTarget || e.Trigger == "0"
	}
	if !seenTarget {
		t.Fatalf("checkpoint must expose target slot 0 after the sibling order ask, got %+v", at.Stack)
	}
}
