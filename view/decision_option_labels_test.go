package view

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestDecisionOptionLabelUsesOnlyProjectedCards(t *testing.T) {
	g := fourSeatBoard(t)
	c, diags := cards.ParseBytes("option-labels.txt", []byte("Name:Visible Exile Card\nTypes:Creature\nPT:1/1\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parsing card: %v", diags)
	}
	c.Link()
	exiled := g.AddObject(c, 1)
	exiled.Zone = state.ZExile
	g.SetZone(state.ZExile, 1, []state.ObjID{exiled.ID})
	hidden := g.AddObject(c, 1)
	hidden.Zone = state.ZHand
	g.SetZone(state.ZHand, 1, append(g.Zone(state.ZHand, 1), hidden.ID))

	d := &decision.Decision{
		Player: 0,
		Options: []decision.Option{
			{Index: 0, Kind: "card", Obj: exiled.ID},
			{Index: 1, Kind: "card", Obj: hidden.ID},
		},
	}
	v := Project(g, flatChars{g}, 0, d)
	if len(v.Players[1].Exile) != 1 || v.Players[1].Exile[0].ID != exiled.ID {
		t.Fatalf("precondition: exile card is not visible in projection: %+v", v.Players[1].Exile)
	}
	if v.Players[1].Hand != nil {
		t.Fatalf("precondition: opponent hand unexpectedly projected: %+v", v.Players[1].Hand)
	}
	if got := v.Decision.Options[0].Label; got != "Visible Exile Card" {
		t.Errorf("visible exile option label = %q, want %q", got, "Visible Exile Card")
	}
	if got := v.Decision.Options[1].Label; got != "" {
		t.Errorf("hidden hand option label = %q, want empty", got)
	}
}
