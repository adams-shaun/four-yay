package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverDetectsSpreeStructurally pins the cast step's Spree handling. XMage
// keeps prompting for further Spree modes after the scripted pick, and only the
// driver's closing [mode_skip] stops it; the old hardcoded SPREE_CARDS name set
// listed 11 of the corpus's 21 Spree cards, so a Spree card missing from it
// (One Last Job among them) never got the skip and XMage failed the scenario
// with "Missing MODE def". The driver must decide from the card itself.
func TestDriverDetectsSpreeStructurally(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)

	if strings.Contains(java, "SPREE_CARDS") {
		t.Errorf("ScenarioReplay.java still hardcodes a SPREE_CARDS name set; detect Spree from the card instead")
	}
	if !strings.Contains(java, "containsClass(SpreeAbility.class)") {
		t.Errorf("ScenarioReplay.java does not detect Spree via SpreeAbility on the card")
	}
	body, ok := caseBody(java, "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}
	if !strings.Contains(body, "isSpreeSpell(") || !strings.Contains(body, "TestPlayer.MODE_SKIP") {
		t.Errorf("cast case does not close the Spree mode prompt via isSpreeSpell + MODE_SKIP:\n%s", body)
	}
}
