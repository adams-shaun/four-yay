package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestRiteOfRenewalParentTargetRestrictsGraveyards(t *testing.T) {
	reg := freshCorpusRegistry(t, "r/rite_of_renewal.txt")
	rite := mustCorpusCard(t, reg, "Rite of Renewal")
	if len(rite.Faces) == 0 {
		t.Fatal("precondition: Rite of Renewal has no face")
	}
	sa := cards.ResolveSVar(rite.Faces[0].SVars, "DBChangeZone")
	if sa == nil || sa.API != "ChangeZone" || sa.Params["TargetsWithDefinedController"] != "ParentTarget" || sa.Params["Origin"] != "Graveyard" || sa.Params["ValidTgts"] != "Card" {
		t.Fatalf("precondition: Rite DBChangeZone SVar = %+v; want ChangeZone targeting Cards in Graveyard with ParentTarget controller", sa)
	}

	e := newSeats(t, 2)
	source := e.G.AddObject(rite, 0).ID
	candidates := []state.ObjID{
		putGraveyard(t, e, 0, "Name:Seat Zero Card\nTypes:Creature\nOracle:x\n"),
		putGraveyard(t, e, 1, "Name:Seat One Card\nTypes:Creature\nOracle:x\n"),
	}
	for _, id := range candidates {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("precondition: candidate %d is not in a graveyard", id)
		}
	}
	controllers := []state.PlayerID{e.G.Obj(candidates[0]).Controller, e.G.Obj(candidates[1]).Controller}
	if controllers[0] == controllers[1] || controllers[0] != 0 || controllers[1] != 1 {
		t.Fatalf("precondition: candidate controllers = %v; want distinct seats 0 and 1", controllers)
	}
	for _, parentSeat := range []state.PlayerID{1, 0} {
		parent := []state.Target{{Player: parentSeat, IsPlayer: true}}
		if !parent[0].IsPlayer || parent[0].Player != parentSeat {
			t.Fatalf("precondition: parent target = %+v; want player %d", parent[0], parentSeat)
		}
		got := e.LegalSubTargets(parentSeat, source, sa, parent)
		if len(got) != 1 || got[0].Obj != candidates[parentSeat] {
			t.Fatalf("parent player %d offered targets %+v; want only graveyard card %d", parentSeat, got, candidates[parentSeat])
		}
	}
}
