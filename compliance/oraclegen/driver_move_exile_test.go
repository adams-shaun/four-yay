package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverMoveMapsExileToXMageEnum pins the `move` op's zone mapping: the
// generator emits To:"exile" for a TgtZone$ Exile ThisTurnEntered filter, and
// XMage's Zone enum has no EXILE member (it is EXILED), so a bare
// Zone.valueOf(upper) throws at replay time.
func TestDriverMoveMapsExileToXMageEnum(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := caseBody(string(b), "move")
	if !ok {
		t.Fatal("ScenarioReplay.java has no move case")
	}
	if !strings.Contains(body, `to.equals("exile") ? Zone.EXILED`) {
		t.Errorf("move case does not map \"exile\" to Zone.EXILED:\n%s", body)
	}
}
