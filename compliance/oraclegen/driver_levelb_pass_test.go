package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

func TestDriverStepSwitchCoversLevelBPass(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	body, ok := caseBody(java, "pass")
	if !ok {
		t.Fatal("ScenarioReplay.java has no pass case")
	}
	for _, required := range []string{"passAction(steps(sc0), stepIdx)"} {
		if !strings.Contains(body, required) {
			t.Errorf("pass case does not contain %q", required)
		}
	}
	for _, required := range []string{
		"static int passAction(JsonArray steps, int index)",
		"static List<String> passCommands(JsonArray steps, int index, int activeSeat)",
		"waitStackResolved(turn, phase, playerA, true)",
		"(info, pl, g) -> pl.pass(g)",
		"PASS_HANDOFF",
		"PASS_PAIR_SECOND",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java does not contain %q", required)
		}
	}
	// Ordering: the replay loop queues a step's pass commands, then the step,
	// then its checkpoint. (The queue order itself is checked by the
	// executable driver test, which mirrors this loop.)
	i := strings.Index(java, "int beforeA = playerA.getActions().size();")
	if i < 0 {
		t.Fatal("ScenarioReplay.java replay loop not found")
	}
	loop := java[i:]
	pc, st, cp := strings.Index(loop, "queuePassCommands(i);"), strings.Index(loop, "step(st, op, i);"), strings.Index(loop, "runCode(cp, turn")
	if pc < 0 || st < pc || cp < st {
		t.Errorf("replay loop must queue pass commands, then the step, then its checkpoint (offsets %d %d %d)", pc, st, cp)
	}
}
