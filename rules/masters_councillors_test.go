package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestSetAudit_hob_MastersCouncillors_CountsGraveyardsWithSevenPlus(t *testing.T) {
	e := layerEngine(t)
	councillor := onBoardCard(t, e, 0, corpusCard(t, "Master's Councillors"))
	graveyardCard := card(t, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for i := 0; i < 7; i++ {
		o := e.G.AddObject(graveyardCard, 0)
		o.Zone = state.ZGraveyard
		e.G.SetZone(state.ZGraveyard, 0, append(e.G.Zone(state.ZGraveyard, 0), o.ID))
	}
	e.staticEpoch = -1

	if obj := e.G.Obj(councillor); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatal("Master's Councillors is not on the battlefield")
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 7 {
		t.Fatalf("controller graveyard has %d cards, want exactly 7", got)
	}
	foundStatic := false
	for _, ce := range e.active() {
		if ce.Source == councillor && ce.AddPowerExpr == "X" {
			foundStatic = true
			break
		}
	}
	if !foundStatic {
		t.Fatal("Master's Councillors AddPower$ X continuous static was not registered")
	}
	if got := e.Derived(councillor).Power; got != 3 {
		t.Fatalf("Master's Councillors power = %d, want 3 (base 1 + 2 for one graveyard holding >=7 cards)", got)
	}
}
