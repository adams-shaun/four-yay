package effects

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"testing"
)

func TestFlipCoinModFirstFlipForcedThenOrdinary(t *testing.T) {
	h := newHost(t, 2)
	edgar := mkCard(t, "Name:Test Edgar\nTypes:Creature Human\nPT:2/2\nS:Mode$ FlipCoinMod | ValidPlayer$ You | CheckSVar$ Count$YouFlipThisTurn | SVarCompare$ EQ0 | Result$ True\nOracle:x\n")
	obj := h.g.AddObject(edgar, 0)
	obj.Zone = state.ZBattlefield
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{obj.ID})
	if h.g.Players[0].CoinFlipsThisTurn != 0 {
		t.Fatal("precondition: player already flipped")
	}
	c := &Ctx{Source: obj.ID, Controller: 0}
	Resolve(h, c, sa(t, "DB$ FlipCoin | Amount$ 2 | RememberResult$ True"))
	if c.FlipMemory == nil || len(c.FlipMemory.Results) != 2 {
		t.Fatalf("precondition: expected two actual flips, got %+v", c.FlipMemory)
	}
	if !c.FlipMemory.Results[0].Heads || h.n != 1 {
		t.Fatalf("first flip should be forced, second random: results=%v draws=%d", c.FlipMemory.Results, h.n)
	}
	if h.g.Players[0].CoinFlipsThisTurn != 2 {
		t.Fatalf("flip tally = %d, want 2", h.g.Players[0].CoinFlipsThisTurn)
	}
	if clone := h.g.Clone(); clone.Players[0].CoinFlipsThisTurn != 2 {
		t.Fatalf("clone lost flip tally: %d", clone.Players[0].CoinFlipsThisTurn)
	}
	h.Emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: h.g.Turn + 1})
	if h.g.Players[0].CoinFlipsThisTurn != 0 {
		t.Fatal("flip tally survived turn change")
	}
}
