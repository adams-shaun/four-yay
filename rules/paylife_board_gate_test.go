package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// A life cost inside a resolution can encounter competing PayLife replacements
// even when the resolving ability's own text poses no decision.
func TestPayLifeCostBoardGateCompetingReplacements(t *testing.T) {
	src := func(name string) *cards.Card {
		return card(t, gateRepl(name,
			"Event$ PayLife | ActiveZones$ Battlefield | ValidPlayer$ You | ReplaceWith$ Ex | Description$ x",
			"Ex:DB$ GainLife | Defined$ You | LifeAmount$ 1"))
	}
	a, b := src("PayLife Gate A"), src("PayLife Gate B")
	e, _ := tokenReplGame(t, 7719, a, b)
	for _, c := range []*cards.Card{a, b} {
		id := moveSeededCard(t, e, 0, c, state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
			t.Fatalf("precondition: %s is not active for the payer: %+v", c.Faces[0].Name, o)
		}
	}
	if got := tapeReplMayAsk(e, "PayLife"); !got {
		t.Fatal("precondition: two PayLife lines must compete")
	}
	if !tapeBoardCompetes(e) {
		t.Fatal("PayLife cost replacement-order ask is not checkpointed")
	}
}
