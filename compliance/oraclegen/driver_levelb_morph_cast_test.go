package oraclegen_test

import (
	"os"
	"strings"
	"testing"
)

// TestDriverCastArmMapsMorphFamilySpelling pins the driver's `cast` arm after
// the morph-family extension: morphed and megamorphed both spell the face-down
// cast "... using Morph" (XMage has NO "using Megamorph"; MorphAbility sets
// SpellAbilityCastMode.MORPH for both families), disguised keeps "... using
// Disguise", and every other cast_mode stays rejected.
func TestDriverCastArmMapsMorphFamilySpelling(t *testing.T) {
	b, err := os.ReadFile(scenarioReplayPath)
	if err != nil {
		t.Fatal(err)
	}
	java := string(b)

	cast, ok := caseBody(java, "cast")
	if !ok {
		t.Fatal("ScenarioReplay.java has no cast case")
	}

	// Precondition: this is the arm that builds the cast command; without the
	// suffix expression the assertions below would be vacuous.
	if !strings.Contains(cast, "String castSuffix") || !strings.Contains(cast, "String card = cardName + castSuffix;") {
		t.Fatalf("cast arm no longer composes the cast command from a suffix:\n%s", cast)
	}

	// The guard must admit all three face-down modes and keep rejecting any
	// other cast_mode.
	for _, mode := range []string{`"morphed"`, `"megamorphed"`, `"disguised"`} {
		if !strings.Contains(cast, mode+".equals(mode)") {
			t.Errorf("cast arm guard does not admit %s", mode)
		}
	}
	if !strings.Contains(cast, `throw new IllegalArgumentException("kicked/cast_mode unsupported")`) {
		t.Error("cast arm no longer rejects an unsupported cast_mode")
	}

	// The suffix: both morph and megamorph map to " using Morph"; only
	// disguised maps to " using Disguise".
	if !strings.Contains(cast, `? " using Morph"`) {
		t.Error(`cast arm does not spell morphed/megamorphed " using Morph"`)
	}
	if !strings.Contains(cast, `? " using Disguise" : "";`) {
		t.Error(`cast arm does not spell disguised " using Disguise"`)
	}
	if strings.Contains(java, "using Megamorph") {
		t.Error(`driver emits "using Megamorph", which XMage does not define`)
	}
}
