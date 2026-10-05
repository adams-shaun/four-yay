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
		"endTurnScenario = hasEndTurnEffect(xmageName.isEmpty() ? gorgeName : xmageName);",
		"return card.getSpellAbility() != null && abilityHasEndTurnEffect(card.getSpellAbility());",
		"for (Ability sub : ability.getSubAbilities())",
		// Generated scenarios omit xmage_name when the two card names match (e.g. FIN Ultima).
		"xmageName = str(sc, \"xmage_name\");", "gorgeName = str(sc, \"card\");",
		"setStopAt(endTurnScenario ? TURN + 1 : TURN,",
		"endTurnScenario ? PhaseStep.UPKEEP : PhaseStep.END_TURN);",
		"unusedActionCount(msg) >= 0",
		"skippedActionsMatch(completedSteps, queuedA, queuedB)",
		"for (int skipped = completedSteps; skipped < stepCount; skipped++)",
		"snaps.add(snapshot(\"step \" + skipped + \" (\" + op + \")\", currentGame));",
		"getActions().equals(remainingA)",
		"getActions().equals(remainingB)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("EndTurn checkpoint fallback contract missing %q", required)
		}
	}
}
