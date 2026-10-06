package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// TestSpellOrPermanentPermanentHalfIsPinned couples the Aang, Swift Savior
// fix to the tracked driver. The base TestPlayer.chooseTarget handles a
// TargetSpellOrPermanent's stack half only and then asserts, so a
// battlefield-permanent answer queued with addTarget (Aang's resolve-step
// airbend, Jeskai Revelation's multi-target cast) can never be matched there.
// The driver must handle that half itself, in the ScriptedChoicePlayer
// override -- never by editing the out-of-tree TestPlayer, which an XMAGE_REF
// bump would discard. Reverting the override must fail here even though the
// generator output is unchanged.
func TestSpellOrPermanentPermanentHalfIsPinned(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		"public boolean chooseTarget(Outcome outcome, mage.target.Target target, Ability source, Game game)",
		"orig instanceof mage.target.common.TargetSpellOrPermanent",
		"!TestPlayer.TARGET_SKIP.equals(getTargets().get(0))",
		"findBattlefieldTarget(target, source, game, getTargets().get(0))",
		"Permanent findBattlefieldTarget",
		"target.possibleTargets(source.getControllerId(), source, game)",
		"Permanent p = game.getPermanent(id)",
		"hasObjectTargetNameOrAlias(p, name)",
		"getTargets().remove(0)",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("ScenarioReplay.java no longer implements the TargetSpellOrPermanent permanent-half contract %q", required)
		}
	}
}
