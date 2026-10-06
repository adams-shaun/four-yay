package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAangEnterTriggerTargetIsQueuedForResolve pins the generated answer shape:
// the cast has no target answer, and resolving Aang queues the ETB's Grizzly
// Bears target. This is an invariant pin, not a generator fix; the driver must
// consume this answer during the resolve action (pinned separately).
func TestAangEnterTriggerTargetIsQueuedForResolve(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	item, skip := Generate(reg, "Aang, Swift Savior")
	if skip != nil {
		t.Fatalf("Aang scenario skipped: %+v", skip)
	}
	steps := item.Scenario.Steps
	if len(steps) != 2 || steps[0].Op != "cast" || steps[1].Op != "resolve" {
		t.Fatalf("unexpected scenario steps: %+v", steps)
	}
	battlefield := item.Scenario.Setup["p1"].Battlefield
	if len(battlefield) != 1 || battlefield[0] != "Grizzly Bears" {
		t.Fatalf("target fixture battlefield = %v, want Grizzly Bears present", battlefield)
	}
	if len(item.XAnswers) != len(steps) {
		t.Fatalf("xmage answers = %+v, want one entry per scenario step", item.XAnswers)
	}
	if len(item.XAnswers[0]) != 0 {
		t.Fatalf("cast-step answers = %+v, want none before the ETB trigger", item.XAnswers[0])
	}
	if got := item.XAnswers[1]; len(got) != 1 || got[0].Kind != "target" || got[0].Value != "Grizzly Bears" {
		t.Fatalf("resolve-step answers = %+v, want ETB target Grizzly Bears", got)
	}
	if got := steps[1].Answers; len(got) != 1 || got[0].Kind != "target" || len(got[0].Pick) != 1 || got[0].Pick[0] != "Grizzly Bears (b)" {
		t.Fatalf("gorge resolve answers = %+v, want Grizzly Bears target", got)
	}
}
