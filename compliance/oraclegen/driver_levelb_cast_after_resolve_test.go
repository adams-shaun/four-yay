package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverCastTargetAfterResolveIsNotSpellOnStack pins the two places the
// driver decides a target names a spell still on the stack. `cast` is the
// list of cast-command names, and before this ticket it accumulated every
// spell ever cast. A single-target cast whose target name was in `cast` was
// therefore taken as "targeting a spell an earlier step cast" and queued the
// `$spellOnStack=` form, which waits for that spell on the stack. After a
// resolve or a resolving pass pair the spell is gone, so the command never
// fires and the queued action is never consumed ("Player PlayerA must have 0
// actions but found N").
//
// The invariant is: `cast` tracks what is still on the stack. A resolve
// empties the stack (clear `cast`); a pass pair resolves its top object
// (drop the last cast). Then the `$spellOnStack` branch is reachable only
// while the target is genuinely still on the stack.
func TestDriverCastTargetAfterResolveIsNotSpellOnStack(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)

	// Precondition: the spell-on-stack branch exists and is what this test is
	// about. Without it the assertions below would be vacuous.
	cast, ok := caseBody(java, "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}
	if !strings.Contains(cast, "castSpell(turn, phase, p, card, spell, spell)") {
		t.Fatalf("cast case no longer queues the $spellOnStack form; the tracking under test is gone")
	}
	if !strings.Contains(cast, "cast.contains(castSpelling(refName(tg.get(0))))") {
		t.Fatal("cast case no longer gates the $spellOnStack branch on cast.contains(...)")
	}

	// The resolve op passes priority until the stack is empty, so every cast
	// recorded on the stack is gone and a later cast targeting one of those
	// names must fall through to its permanent alias.
	resolve, ok := caseBody(java, "resolve")
	if !ok {
		t.Fatal("ScenarioReplay.java has no resolve case")
	}
	if !strings.Contains(resolve, "waitStackResolved(turn, phase, p)") {
		t.Fatal("resolve case no longer waits for the stack to empty")
	}
	if !strings.Contains(resolve, "cast.clear()") {
		t.Errorf("resolve case must clear `cast`: the stack it emptied no longer holds those spells")
	}

	// The pass pair resolves the stack's top object, so the cast it spent is
	// no longer on the stack.
	for _, required := range []string{
		"dropLastCast()",
		"private void dropLastCast()",
		"cast.remove(cast.size() - 1)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java does not contain %q", required)
		}
	}
	wait := strings.Index(java, "case CMD_WAIT_RESOLVE_ONE:")
	if wait < 0 {
		t.Fatal("queuePassCommands has no CMD_WAIT_RESOLVE_ONE case")
	}
	waitEnd := strings.Index(java[wait:], "break;")
	if waitEnd < 0 {
		t.Fatal("the CMD_WAIT_RESOLVE_ONE case has no break")
	}
	if !strings.Contains(java[wait:wait+waitEnd], "dropLastCast()") {
		t.Error("the resolving pass pair must drop the last cast before its checkpoint")
	}

	// The queued-target path has the same stale test, so it must read the
	// same `cast` list (fixed by the tracking, not by a parallel list).
	queueAt := strings.Index(java, "private void queueCastTarget(TestPlayer p, String t)")
	if queueAt < 0 {
		t.Fatal("ScenarioReplay.java has no queueCastTarget")
	}
	queue := java[queueAt:]
	if !strings.Contains(queue[:400], "cast.contains(castSpelling(refName(t)))") {
		t.Error("queueCastTarget no longer gates the spell-on-stack form on cast.contains(...)")
	}
}
