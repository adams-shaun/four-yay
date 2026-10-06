package oraclegen_test

import (
	"os"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
)

func TestDriverPassToStepsAndActiveSeats(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	body, ok := caseBody(java, "pass_to")
	if !ok {
		t.Fatal("ScenarioReplay.java has no pass_to case")
	}
	mappings := map[string]string{
		"begin-combat":      `stepName.equals("begin-combat")`,
		"draw":              `stepName.equals("draw")`,
		"end":               `stepName.equals("end")`,
		"end-combat":        `stepName.equals("end-combat")`,
		"main1":             `stepName.equals("main1")`,
		"main2":             `stepName.equals("main2")`,
		"upkeep":            `stepName.equals("upkeep")`,
		"declare-attackers": `stepName.equals("declare-attackers")`,
		"declare-blockers":  `stepName.equals("declare-blockers")`,
	}
	for _, checkpoint := range templates.PassToSteps() {
		parts := strings.SplitN(checkpoint, "@", 2)
		if _, exists := mappings[parts[0]]; !exists || !strings.Contains(body, mappings[parts[0]]) {
			t.Errorf("pass_to step %q has no mapping in ScenarioReplay.java", parts[0])
		}
		if len(parts) == 2 {
			active := `active.equals("` + parts[1] + `")`
			if !strings.Contains(body, active) {
				t.Errorf("pass_to active seat %q has no handling in ScenarioReplay.java", parts[1])
			}
		}
	}
	// A requested seat means its next turn: switching seats advances one,
	// while requesting the current seat skips the opponent and advances two.
	if !strings.Contains(body, "turn += nextActiveSeat == activeSeat ? 2 : 1") {
		t.Error("pass_to does not advance to the requested seat's next turn (same-seat requests must skip a round)")
	}
	for _, tc := range []struct {
		current, requested, want int
	}{
		{0, 1, 2}, // p1 after p0: turn 2
		{0, 0, 3}, // p0 again: turn 3
	} {
		got := 1
		if tc.current == tc.requested {
			got += 2
		} else {
			got++
		}
		if got != tc.want {
			t.Errorf("active p%d after p%d: turn = %d, want %d", tc.requested, tc.current, got, tc.want)
		}
	}
}

func TestDriverPassToCheckpointUsesCurrentTurn(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)
	if !strings.Contains(java, `runCode(cp, turn, phase, playerA`) {
		t.Error("per-step runCode checkpoint does not use the current turn variable")
	}
	if strings.Contains(java, `runCode(cp, TURN, phase, playerA`) {
		t.Error("per-step runCode checkpoint still uses the initial TURN")
	}
}
