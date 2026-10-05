package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	costvocab "github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func TestCraftMaterialCostObjectsPersistOnSource(t *testing.T) {
	baseline := "Name:Source\nManaCost:0\nTypes:Artifact\nOracle:x\n"
	e, _, source := newFixtureDeck(t, 9101, baseline)
	e.emit(events.Event{Kind: events.MoveZone, Obj: source, From: state.ZLibrary, To: state.ZBattlefield})
	battlefield := putBattlefield(t, e, 0, "Name:BF Material\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	graveyard := putGraveyard(t, e, 0, "Name:GY Material\nManaCost:0\nTypes:Artifact\nOracle:x\n")
	if e.G.Obj(battlefield).Zone != state.ZBattlefield || e.G.Obj(graveyard).Zone != state.ZGraveyard {
		t.Fatal("precondition: candidate materials are not in the specified zones")
	}
	pc := &pendingCast{card: source, player: 0,
		cost:        costvocab.ParseCost("ExileCtrlOrGrave<1/Artifact.Other>"),
		CastPayment: pay.CastPayment{}, PaidCost: pay.PaidCost{Exiles: []state.ObjID{battlefield, graveyard}}}
	got := craftExiledMaterials(e, pc)
	if len(got) != 2 || got[0] != battlefield || got[1] != graveyard {
		t.Fatalf("Craft selected materials = %v, want battlefield %d then graveyard %d", got, battlefield, graveyard)
	}
	rememberCraftMaterials(e, source, got)
	remembered := e.G.Obj(source).Remembered
	if len(remembered) != 2 || remembered[0].Obj != battlefield || remembered[1].Obj != graveyard {
		t.Fatalf("persistent source Remembered = %+v, want both paid materials", remembered)
	}
}
