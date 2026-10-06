package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// XMage cannot start in the seat, so pin the setup callback and its production
// helper: setup counters must be followed by a layer refresh before snapshot
// serializes P/T. The executable XMage-side test covers the helper's result.
func TestDriverReappliesSetupCountersBeforeSnapshot(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)

	callbackStart := strings.Index(java, `runCode("setup", TURN, MAIN, playerA, (info, p, g) -> {`)
	if callbackStart < 0 {
		t.Fatal("ScenarioReplay.java has no setup snapshot callback")
	}
	callbackEnd := strings.Index(java[callbackStart:], "            });")
	if callbackEnd < 0 {
		t.Fatal("setup snapshot callback is unterminated")
	}
	callback := java[callbackStart : callbackStart+callbackEnd]
	counterAt := strings.Index(callback, "applySetupState(g);")
	aliasAt := strings.Index(callback, "registerAliases(g);")
	snapshotAt := strings.Index(callback, "snaps.add(snapshot(info, g));")
	if counterAt < 0 || aliasAt < 0 || snapshotAt < 0 || !(counterAt < aliasAt && aliasAt < snapshotAt) {
		t.Fatalf("setup must apply counters before aliases and snapshot; callback:\n%s", callback)
	}

	helperStart := strings.Index(java, "private void applySetupState(Game g) {")
	if helperStart < 0 {
		t.Fatal("setup state helper is missing")
	}
	helperEnd := strings.Index(java[helperStart:], "\n    }")
	if helperEnd < 0 {
		t.Fatal("setup state helper is unterminated")
	}
	helper := java[helperStart : helperStart+helperEnd]
	addAt := strings.Index(helper, "perm.addCounters(")
	applyAt := strings.Index(helper, "g.applyEffects();")
	if addAt < 0 || applyAt < 0 || addAt >= applyAt {
		t.Fatalf("setup state helper must add counters and then apply effects; helper:\n%s", helper)
	}
}
