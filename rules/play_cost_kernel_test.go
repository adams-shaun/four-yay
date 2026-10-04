package rules

// Kernel-era restorations of the tests W3 removed from play_cost_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestAmpedRaptorPlayCostPaysEnergyInsteadOfManaKernel(t *testing.T) {
	t.Parallel()
	e, bearID := energyPlayFixture(t)
	// The pool is empty: the only way the YES arm can pay is the {E}
	// alternative. Answer YES (option 0).
	submitChoices(t, e, 0)
	kr7Settle(e)
	passUntilStackEmpty(t, e, 60)
	if n := e.G.Players[0].Counter("ENERGY"); n != 0 {
		t.Fatalf("energy after the YES arm = %d, want 0 (PayEnergy<ConvertedManaCost> paid)", n)
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("mana pool after the YES arm = %d, want 0 (the mana cost is never charged)", pool)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played card is %v, want on the battlefield", o)
	}
	if hasNote(e, "declined") {
		t.Fatalf("the YES answer was declined although the {E} alternative was payable")
	}
}
