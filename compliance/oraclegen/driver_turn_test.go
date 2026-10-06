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
		// The parser defaults absent/null to 1, like gorge's *int field.
		"TURN = scenarioTurn(sc);",
		"if (value == null || value.isJsonNull()) {\n            return 1;",
		// Gson getAsInt would truncate fractions and wrap overflow. Reject
		// both, and reject strings rather than accepting a quoted number.
		"value.getAsJsonPrimitive().isNumber()",
		"value.getAsString().matches(\"-?[0-9]+\")",
		"int turn = Integer.parseInt(value.getAsString());",
		"if (turn >= 1 && turn <= 100) {",
		"throw new IllegalArgumentException(\"invalid scenario turn \" + value + \" (want integer 1..100)\");",
		// Setup establishes the board at the requested scenario turn; the
		// final stop follows the last pass_to turn.
		"runCode(\"setup\", TURN, MAIN, playerA, (info, p, g) -> {",
		"setStopAt(endTurnScenario ? turn + 1 : turn,",
		// A valid upper-bound turn must not exhaust a fixed 40-card library.
		// Keep turn-1 padding unchanged, and mirror gorge's later draw reserve.
		"int filler = Math.max(0, 40 - named);",
		"if (TURN > 1) {\n                filler = Math.max(filler, TURN / 2 + 1);",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer consumes the scenario turn contract %q", required)
		}
	}
	if strings.Contains(java, "private static final int TURN") {
		t.Error("ScenarioReplay.java made TURN static again: one scenario's turn would leak into the next in a batch")
	}
}
