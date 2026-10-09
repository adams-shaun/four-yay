package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// XMage cannot start in the seat, so pin the driver source: the setup
// counters must be applied WITH the placement (through the enter-with-counters
// map, before any state-based action), mirroring gorge's runner order
// (agent 20261009T041408Z, cluster C1). Before this, applySetupState ran
// inside the setup checkpoint -- after the game's first SBA check had already
// killed a would-die placement (a printed 0/0, an X-cost creature's X=0
// board) -- and threw "counters name <card>, which is not on p0's
// battlefield" (the 14-row harness class).
func TestDriverAppliesSetupCountersWithPlacement(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)

	// The placement loop applies the counters after the back-face staging, so
	// a staged placement (a face-1 combat row) is keyed by the card the
	// placement list now holds, and through XMage's enter-with-counters map,
	// which the placement path consumes before the first SBA check.
	addStart := strings.Index(java, "private int add(JsonObject s, String key, Zone zone, TestPlayer p) {")
	if addStart < 0 {
		t.Fatal("ScenarioReplay.java has no add() placement helper")
	}
	addEnd := strings.Index(java[addStart:], "\n    }")
	if addEnd < 0 {
		t.Fatal("add() is unterminated")
	}
	addFn := java[addStart : addStart+addEnd]
	stagingAt := strings.Index(addFn, "stageBackFace(p, n);")
	applyAt := strings.Index(addFn, "setEnterWithCounters(placedCard.getId(), enter)")
	if stagingAt < 0 || applyAt < 0 || stagingAt > applyAt {
		t.Fatalf("add() must apply the setup counters after the back-face staging; add():\n%s", addFn)
	}
	for _, want := range []string{
		`counters.get(n)`,                  // the scenario's counters JSON, by the placed name
		"currentGame.setEnterWithCounters", // the map the placement path consumes
		"enterWithCountersApplied.add",     // applySetupState must not re-add
		"setupBattlefield.merge",           // the entry-history record is unchanged
	} {
		if !strings.Contains(addFn, want) {
			t.Fatalf("add() lost %s; add():\n%s", want, addFn)
		}
	}

	// build() resets the applied set per scenario, so one scenario's applied
	// ids cannot leak into the next.
	buildStart := strings.Index(java, "private void build(JsonObject sc) {")
	if buildStart < 0 {
		t.Fatal("ScenarioReplay.java has no build()")
	}
	buildEnd := strings.Index(java[buildStart:], "\n    }")
	if buildEnd < 0 {
		t.Fatal("build() is unterminated")
	}
	if !strings.Contains(java[buildStart:buildStart+buildEnd], "enterWithCountersApplied.clear();") {
		t.Fatal("build() must reset the placement-applied counters set")
	}

	// applySetupState keeps its loud unknown-name behaviour but skips the
	// permanents the placement already counted, and still recomputes effects
	// when it adds anything itself (counter-gated statics).
	helperStart := strings.Index(java, "private void applySetupState(Game g) {")
	if helperStart < 0 {
		t.Fatal("setup state helper is missing")
	}
	helperEnd := strings.Index(java[helperStart:], "\n    }")
	if helperEnd < 0 {
		t.Fatal("setup state helper is unterminated")
	}
	helper := java[helperStart : helperStart+helperEnd]
	if !strings.Contains(helper, "enterWithCountersApplied.contains(perm.getId())") {
		t.Fatalf("applySetupState must skip the placement-applied permanents; helper:\n%s", helper)
	}
	if !strings.Contains(helper, "counters name ") {
		t.Fatalf("applySetupState must keep the loud unknown-name throw; helper:\n%s", helper)
	}
	helperAddAt := strings.Index(helper, "perm.addCounters(")
	helperApplyAt := strings.Index(helper, "g.applyEffects();")
	if helperAddAt < 0 || helperApplyAt < 0 || helperAddAt >= helperApplyAt {
		t.Fatalf("setup state helper must add counters and then apply effects; helper:\n%s", helper)
	}
}
