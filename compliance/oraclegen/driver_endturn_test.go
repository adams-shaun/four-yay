package oraclegen

import (
	"os"
	"strings"
	"testing"
)

func TestScenarioReplayEndTurnSnapshotsAreConditionalAndComplete(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"endTurnScenario = hasEndTurnEffect(xmageName);",
		"setStopAt(endTurnScenario ? TURN + 1 : TURN,",
		"endTurnScenario ? PhaseStep.UPKEEP : PhaseStep.END_TURN);",
		"int skippedSteps = stepCount - completedSteps;",
		"int unusedActions = unusedActionCount(msg);",
		"unusedActions == skippedSteps",
		"for (int skipped = completedSteps; skipped < stepCount; skipped++)",
		"snaps.add(snapshot(\"step \" + skipped + \" (\" + op + \")\", currentGame));",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("EndTurn checkpoint fallback contract missing %q", required)
		}
	}
}
