package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// TestXMageTargetSkipDriverIsPinned couples the explicit-skip contract to the
// tracked driver. XMage cannot run in a seat (its H2 database fails to
// initialise in the jail), so this is a STRUCTURAL pin: it checks the source
// text, and the behaviour is proved by the operator's strict host replay of
// Rise from the Wreck. The driver must (a) read xmage_target_skips and queue
// each skip at its offset with no blind trailing skip, and (b) hide a skip
// that belongs to a LATER target object from TestPlayer.chooseTarget, whose
// zone matcher rejects a "[target_skip]" it scans past -- in the
// ScriptedChoicePlayer override, never in the out-of-tree TestPlayer.
func TestXMageTargetSkipDriverIsPinned(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		`sc.has("xmage_target_skips")`,
		"castTargetSkipsAt(stepIdx, tg.size())",
		"queueCastTargetsWithSkips(p, tg, skips)",
		"addTarget(p, TestPlayer.TARGET_SKIP);",
		"static List<String> hideAfterNextSkip(List<String> queue)",
		"queue.indexOf(TestPlayer.TARGET_SKIP)",
		"getTargets().addAll(later)",
		"chooseTargetInSegment(outcome, target, source, game)",
		"is out of order or beyond its",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer implements the explicit target-skip contract %q", required)
		}
	}
	// The explicit plan must not fall through to the legacy trailing skip:
	// its branch precedes the adjusted/single/multi-target routes and returns
	// through the shared cast.add at the end of the case.
	plan := strings.Index(java, "queueCastTargetsWithSkips(p, tg, skips);")
	legacy := strings.Index(java, "if (!singleTargetFilled(card, tg.size()))")
	if plan < 0 || legacy < 0 || plan > legacy {
		t.Errorf("the explicit skip branch (%d) must precede the legacy trailing skip (%d)", plan, legacy)
	}
}
