package oraclegen

import (
	"os"
	"strings"
	"testing"
)

// XMage's ManaPool.getWhite() and the other colour getters sum only
// unconditional pool items; restricted mana (DFT Boommobile, HOB Desolation of
// Smaug) is a ConditionalMana item. The driver's pool snapshot must read
// getConditionalMana() too, or the comparator sees "" where gorge sees "WWWW".
// The executable check is scripts/xmage-oracle-test-pool.sh (needs the host's
// XMage build); this pins the source shape where seats cannot run it.
func TestDriverPoolSnapshotCountsConditionalMana(t *testing.T) {
	source, err := os.ReadFile("../../tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java")
	if err != nil {
		t.Fatal(err)
	}
	java := string(source)
	start := strings.Index(java, "static String pool(ManaPool mp)")
	if start < 0 {
		t.Fatal("driver has no pool(ManaPool) snapshot")
	}
	body := java[start:]
	if end := strings.Index(body, "private static void rep("); end >= 0 {
		body = body[:end]
	}
	for _, required := range []string{
		"mp.getConditionalMana()",
		"cm.getWhite()", "cm.getBlue()", "cm.getBlack()", "cm.getRed()", "cm.getGreen()",
		"cm.getColorless()", "cm.getGeneric()",
		"rep(b, 'W', w);", "rep(b, 'C', c);",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("pool snapshot does not count conditional mana: missing %q", required)
		}
	}
	if _, err := os.Stat("../../tools/xmageoracle/test/org/mage/test/oracle/ScenarioReplayPoolTest.java"); err != nil {
		t.Errorf("executable pool test missing: %v", err)
	}
}
