package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticEquipmentCountsETBKills: Chainsaw's "equipped creature gets +X/+0,
// where X is the number of rev counters" counts the creatures its own ETB
// (3 damage to up to one target creature) kills. The ETB target is optional,
// so the probe-preserving path declined it and no creature died. When
// declining leaves the static unobserved the target is redirected to p1's
// probe: the Bear dies, Chainsaw's "one or more creatures die" trigger puts a
// rev counter on it, and p0's attached probe reads +1/+0. The precondition
// asserts a rev counter really is on Chainsaw, p1's probe is gone, and p0's
// probe survived and carries the pump.
func TestStaticEquipmentCountsETBKills(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Chainsaw"
	_, final := selfFilterItem(t, reg, name, "static#0.0")
	saw, ok := permNamedSnap(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if saw.Counters["REV"] != 1 {
		t.Fatalf("%s counters = %v, want one REV counter from the ETB kill", name, saw.Counters)
	}
	var mine, theirs int
	pt := ""
	for _, p := range final.Permanents {
		if p.Name != "Grizzly Bears" {
			continue
		}
		if p.Controller == 0 {
			mine++
			pt = p.PT
		} else {
			theirs++
		}
	}
	if theirs != 0 || mine != 1 {
		t.Fatalf("Bears p0=%d p1=%d, want p0's kept and p1's killed by the ETB", mine, theirs)
	}
	if pt != "3/2" {
		t.Fatalf("p0's Bear P/T = %s, want 3/2 (2/2 plus +1/+0 for one rev counter)", pt)
	}
	if !slices.Contains(saw.Types, "Equipment") {
		t.Fatalf("%s types = %v", name, saw.Types)
	}
}
