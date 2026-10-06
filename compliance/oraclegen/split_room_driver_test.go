package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// TestScenarioReplayCastsSplitHalves pins the driver contract a split or
// Room half needs: setup deals the WHOLE card ("A // B") into XMage, but the
// cast step names the HALF. XMage's SplitCard builds each half's SpellAbility
// from the set-info name split on " // " ("Cast <half>"), while the physical
// card added to a zone is named "A // B", so casting the whole name fails
// with "Can't find ability to activate command: Cast <half>". A seat cannot
// run XMage to prove it, so the two spellings are pinned here at the source.
func TestScenarioReplayCastsSplitHalves(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	for _, required := range []string{
		// The card OBJECT (zone placement, target alias) uses the whole name.
		"addCard(zone, p, xmageSpelling(n), 1, tapped.contains(n));",
		"return (!xmageName.isEmpty() && n.equals(gorgeName)) ? xmageName : n;",
		// The CAST command uses the half name for an "A // B" card.
		"private String castSpelling(String n) {",
		"xmageName.contains(\" // \")",
		"String card = castSpelling(refName(str(st, \"card\")));",
	} {
		if !strings.Contains(java, required) {
			t.Errorf("split/Room cast contract missing %q", required)
		}
	}
}
