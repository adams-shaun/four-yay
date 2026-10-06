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
	for _, required := range []string{
		"passAction(steps(sc0), stepIdx)",
		"waitStackResolved(turn, phase, p, true)",
		"PASS_HANDOFF",
		"PASS_SECOND",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("pass case does not contain %q", required)
		}
	}
	if !strings.Contains(java, "static int passAction(JsonArray steps, int index)") {
		t.Error("pass step patterns are not validated by passAction")
	}
}
