package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// A Mill body with no intrinsic question still needs a checkpoint: two
// count rewrites can ask the affected player for CR 616.1 ordering.
func TestMillBoardGateCompetingReplacementsAsk(t *testing.T) {
	if events, ok := cards.AskFreeAPIEvents("Mill"); !ok || !events.Has(cards.ReplMill) {
		t.Fatal("precondition: ask-free Mill must propose the Mill replacement class")
	}
	water := card(t, gateRepl("Gate Mill Plus", "Event$ Mill | ActiveZones$ Battlefield | ValidPlayer$ Player.Opponent | ReplaceWith$ X | Description$ x",
		"X:DB$ ReplaceEffect | VarName$ Number | VarValue$ Y", "Y:ReplaceCount$Number/Plus.4"))
	bruvac := card(t, gateRepl("Gate Mill Twice", "Event$ Mill | ActiveZones$ Battlefield | ValidPlayer$ Player.Opponent | ReplaceWith$ X | Description$ x",
		"X:DB$ ReplaceEffect | VarName$ Number | VarValue$ Y", "Y:ReplaceCount$Number/Twice"))
	spell := card(t, "Name:Gate Mill Two\nManaCost:0\nTypes:Sorcery\nA:SP$ Mill | Defined$ Opponent | NumCards$ 2 | SpellDescription$ x\nOracle:x\n")
	millGate := mayAskKnown | uint32(cards.ReplEventMask(1)<<cards.ReplMill)<<mayAskGateShift
	withoutSources, _ := tokenReplGame(t, 4244, spell)
	if mayAskOnBoard(withoutSources, millGate) {
		t.Fatal("no Mill replacement sources must not open the Mill gate")
	}
	e, cfg := tokenReplGame(t, 4243, water, bruvac, spell)
	for _, c := range []*cards.Card{water, bruvac} {
		id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on battlefield", c.Faces[0].Name)
		}
	}
	if !mayAskOnBoard(e, millGate) {
		t.Fatal("two battlefield Mill replacements must open the order-choice gate")
	}
	moveSeededCard(t, e, 0, spell, state.ZHand)
	addMana(t, e, 0, "")
	castSpellOption(t, e, "Gate Mill Two")
	if asks := driveCountingReplacementAsks(t, e, 0); asks == 0 {
		t.Fatal("no CR 616.1 mill replacement-order ask was posed")
	}
	replayCheck(t, e, cfg)
}
