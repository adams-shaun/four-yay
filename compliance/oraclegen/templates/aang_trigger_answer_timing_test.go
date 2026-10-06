package templates

import (
	"testing"
)

// TestAangEnterTriggerTargetIsQueuedBeforeCast pins XMage's action timing:
// resolving the newly cast permanent and putting its ETB trigger on the stack
// happen during the cast action, so the trigger target answer must already be
// queued then, not left for the following gorge resolve step.
func TestAangEnterTriggerTargetIsQueuedBeforeCast(t *testing.T) {
	reg := loadGenRegistry(t)
	item, skip := Generate(reg, "Aang, Swift Savior")
	if skip != nil {
		t.Fatalf("Aang scenario skipped: %+v", skip)
	}
	if len(item.Scenario.Steps) != 2 || item.Scenario.Steps[0].Op != "cast" || item.Scenario.Steps[1].Op != "resolve" {
		t.Fatalf("unexpected scenario steps: %+v", item.Scenario.Steps)
	}
	if got := item.Scenario.Setup["p1"].Battlefield; len(got) != 1 || got[0] != "Grizzly Bears" {
		t.Fatalf("target fixture battlefield = %v, want Grizzly Bears present", got)
	}
	if len(item.XAnswers) != 2 {
		t.Fatalf("xmage answers = %+v, want one entry per scenario step", item.XAnswers)
	}
	if len(item.XAnswers[0]) != 1 || item.XAnswers[0][0].Kind != "target" || item.XAnswers[0][0].Value != "Grizzly Bears" {
		t.Fatalf("cast-step answers = %+v, want the ETB target Grizzly Bears", item.XAnswers[0])
	}
	if len(item.XAnswers[1]) != 0 {
		t.Fatalf("resolve-step answers = %+v, want none (answer was consumed while casting)", item.XAnswers[1])
	}
	if len(item.Scenario.Steps[1].Answers) != 1 || item.Scenario.Steps[1].Answers[0].Kind != "target" {
		t.Fatalf("gorge scenario lost resolve target decision: %+v", item.Scenario.Steps[1].Answers)
	}
}
