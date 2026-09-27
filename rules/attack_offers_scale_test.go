package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestAttackOffersStaticScanIsNotPerObject pins the combat half of the
// cardfuzz hang 6181111140895991800 profile: attackOffers (the declare-
// attackers offer list, re-derived by validateAttackers) reads each
// candidate attacker's requirement, goad and restriction statics, and each
// read is a whole-battlefield activeStatics scan outside a memo scope --
// O(creatures^2) per offer build on a 16k-goblin board. The walk is a pure
// read, so one memo scope must serve those scans once per call: the
// per-call allocations (each rescan appending a fresh view slice for the
// board's Continuous static) must not grow with the board.
func TestAttackOffersStaticScanIsNotPerObject(t *testing.T) {
	allocs := func(n int) float64 {
		e := layerEngine(t)
		e.G.Step = state.StepDeclareAttackers
		onBoard(t, e, 0, "Name:Anthem Stand-in\nTypes:Enchantment\n"+
			"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | Description$ Creatures you control get +1/+0.\nOracle:x\n")
		for i := 0; i < n; i++ {
			onBoardReady(t, e, 0, fmt.Sprintf("Name:Goblin %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
		}
		want := len(e.attackOffers()) // verify mode: every cache hit recomputed
		if want != n {
			t.Fatalf("n=%d: %d attack offers, want one per goblin", n, want)
		}
		return allocsWithoutWalkCacheVerify(3, func() {
			if got := len(e.attackOffers()); got != want {
				t.Fatalf("n=%d: attackOffers not stable: %d vs %d", n, got, want)
			}
		})
	}
	small, large := allocs(50), allocs(400)
	// Measured: a per-object whole-board rescan allocated ~2 slices per
	// goblin (114 at 50, 823 at 400); with the scans shared the offer build
	// is nearly flat (14 at 50, 23 at 400 -- the offer list's own growth).
	perObj := (large - small) / 350
	t.Logf("attackOffers allocations: %.0f at 50 goblins, %.0f at 400", small, large)
	if perObj > 0.5 {
		t.Fatalf("attackOffers allocates %.2f per goblin (%.0f at 50, %.0f at 400) -- the static scans are per object", perObj, small, large)
	}
}
