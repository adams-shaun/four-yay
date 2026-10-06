package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// A skipped cast with mana has at least three queued actions: pool, cast,
// checkpoint. Counting skipped steps instead of queued actions loses it.
// XMage cannot run in the sandbox, so pin the driver's queue boundary.
func TestScenarioReplayEndTurnTracksMultiActionSkippedStep(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	queue := java[strings.Index(java, "JsonObject replayOnce("):strings.Index(java, "private static int unusedActionCount(")]
	for _, required := range []string{
		"int beforeA = playerA.getActions().size();",
		"int beforeB = playerB.getActions().size();",
		"step(st, op, i);",
		"runCode(cp, TURN, phase, playerA,",
		"playerA.getActions().subList(beforeA, playerA.getActions().size())",
		"playerB.getActions().subList(beforeB, playerB.getActions().size())",
		"remainingA.addAll(queuedA.get(i));",
		"remainingB.addAll(queuedB.get(i));",
	} {
		if !strings.Contains(queue, required) {
			t.Errorf("multi-action step queue not accounted for: missing %q", required)
		}
	}
	// Check the queue capture surrounds the whole step (and its checkpoint),
	// not just the checkpoint; cast-with-mana has more than one action.
	for _, ordered := range [][]string{
		{"int beforeA =", "step(st, op, i);", "runCode(cp, TURN", "queuedA.add("},
		{"int beforeB =", "step(st, op, i);", "runCode(cp, TURN", "queuedB.add("},
	} {
		at := 0
		for _, s := range ordered {
			n := strings.Index(queue[at:], s)
			if n < 0 {
				t.Errorf("step action capture is not ordered: %v", ordered)
				break
			}
			at += n + len(s)
		}
	}
	cast := java[strings.Index(java, "case \"cast\":"):strings.Index(java, "case \"play\":")]
	if !strings.Contains(cast, "runCode(\"mana \" + mana") || !strings.Contains(cast, "castSpell(TURN, phase, p, card") {
		t.Fatal("test requires a cast-with-mana that schedules actions besides its checkpoint")
	}
	if strings.Contains(queue, "unusedActions == skippedSteps") {
		t.Fatal("a skipped multi-action step cannot be counted as one action")
	}
}
