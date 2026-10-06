package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// TestScenarioReplayConsumesScenarioTurn pins the XMage driver end of the
// scenario turn option. XMage cannot run in a seat, so this source contract is
// the only in-tree check that the driver reads the same `turn` field gorge
// does: it must default to 1, validate 1..100, and use the value for BOTH the
// setup anchor and the stop-at checkpoint. A driver that ignored the field, or
// used it for only one of the two anchors, would compare snapshots taken at
// different turns and every later generated scenario would silently misalign.
func TestScenarioReplayConsumesScenarioTurn(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		// Per-scenario state, not a compile-time constant: the JVM serves a
		// whole batch, so a static TURN would leak one scenario's turn into
		// the next.
		"private int TURN = 1;",
		// Default to turn 1 for every scenario that omits the field.
		"TURN = sc.has(\"turn\") ? sc.get(\"turn\").getAsInt() : 1;",
		// Reject an out-of-range value rather than silently clamping.
		"if (TURN < 1 || TURN > 100) {",
		"throw new IllegalArgumentException(\"invalid scenario turn \" + TURN + \" (want 1..100)\");",
		// Both anchors must use TURN: setup establishes the board at the
		// requested turn, and the stop-at checkpoint snapshots the same one.
		"runCode(\"setup\", TURN, MAIN, playerA, (info, p, g) -> {",
		"setStopAt(endTurnScenario ? TURN + 1 : TURN,",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer consumes the scenario turn contract %q", required)
		}
	}
	if strings.Contains(java, "private static final int TURN") {
		t.Error("ScenarioReplay.java made TURN static again: one scenario's turn would leak into the next in a batch")
	}
}
