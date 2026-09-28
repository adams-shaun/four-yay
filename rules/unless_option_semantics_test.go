package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A decline-only offer stays a decline even if its cost becomes payable after
// it was posed. Index 0 is only the selection identifier, not payment meaning.
func TestUnlessPayDeclineRemainsDeclineWhenCostBecomesPayable(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, bearID := counterFixture(t, reg, "Mana Leak", "Grizzly Bears")
	e.G.Players[0].Pool = state.Mana{}
	islandID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Island"))
	if e.G.Obj(islandID).Zone != state.ZBattlefield || !e.untappedManaSource(0, islandID) {
		t.Fatal("precondition: payer needs an untapped Island on the battlefield")
	}
	if e.UnlessCostPayable(0, "3") {
		t.Fatal("precondition: one Island and an empty pool must not reach {3}")
	}

	ask := drainUntilUnlessPay(t, e, 30)
	if ask == nil || len(ask.Options) != 1 {
		t.Fatalf("precondition: expected decline-only unless-pay ask, got %+v", ask)
	}
	decline := ask.Options[0]
	if decline.Mode != decision.ModeUnlessDecline || decline.Label != "Don't pay" {
		t.Fatalf("sole option must explicitly mean decline without changing its label: %+v", decline)
	}
	if e.G.Obj(bearID).Zone != state.ZStack || e.G.Players[0].Pool[state.MG] != 0 {
		t.Fatal("precondition: creature spell must be on stack and payer pool empty")
	}

	// Change the gate after the option list has been posed. The pool value is
	// now enough for Mana Leak's {3}; choosing the sole offered decline must
	// not charge it.
	e.G.Players[0].Pool[state.MG] = 3
	if !e.UnlessCostPayable(0, "3") {
		t.Fatal("precondition: Mana Leak cost should now be payable")
	}
	submitChoices(t, e, decline.Index)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bearID).Zone != state.ZGraveyard {
		t.Fatalf("declining must let Mana Leak counter the spell; bear zone = %s", e.G.Obj(bearID).Zone)
	}
	if got := e.G.Players[0].Pool[state.MG]; got != 3 {
		t.Fatalf("declining charged mana: pool = %d, want 3", got)
	}
}
