package effects

import (
	"github.com/adams-shaun/gorge/state"
	"testing"
)

// TestFlipCoinModScopesToController pins the ValidPlayer$ You scoping of the
// FlipCoinMod static: only a static whose source is controlled by the flipper
// applies. Edgar is on seat 0's battlefield under seat 0's control and seat 0
// has not flipped this turn, so seat 0's own first flip is forced; a flip for
// seat 1 on that same board must fall through to the RNG instead of inheriting
// seat 0's Edgar.
func TestFlipCoinModScopesToController(t *testing.T) {
	h := newHost(t, 2)
	edgar := mkCard(t, "Name:Test Edgar\nTypes:Creature Human\nPT:2/2\nS:Mode$ FlipCoinMod | ValidPlayer$ You | CheckSVar$ Count$YouFlipThisTurn | SVarCompare$ EQ0 | Result$ True\nOracle:x\n")
	obj := h.g.AddObject(edgar, 0)
	obj.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{obj.ID})

	// Precondition: the static is present and controlled by seat 0, and neither
	// seat has flipped this turn, so seat 0's first flip is in the forced window.
	if obj.Controller != 0 {
		t.Fatalf("precondition: Edgar controller = %d, want 0", obj.Controller)
	}
	if h.g.Players[0].CoinFlipsThisTurn != 0 || h.g.Players[1].CoinFlipsThisTurn != 0 {
		t.Fatalf("precondition: flip tally = (%d, %d), want (0, 0)",
			h.g.Players[0].CoinFlipsThisTurn, h.g.Players[1].CoinFlipsThisTurn)
	}

	// Same board: seat 0's own first flip is forced -- no RNG draw.
	if !FlipCoinWin(h, 0) {
		t.Fatal("seat 0 first flip not forced despite Edgar under its control")
	}
	if h.n != 0 {
		t.Fatalf("seat 0 forced flip consumed %d draws, want 0", h.n)
	}

	// Seat 1 does not control Edgar, so its first flip is not scoped by the
	// modifier and must be decided by the RNG (one draw).
	FlipCoinWin(h, 1)
	if h.n != 1 {
		t.Fatalf("seat 1 flip consumed %d draws, want 1 (Edgar must not force another player's flip)", h.n)
	}
}
