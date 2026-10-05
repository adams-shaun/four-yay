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

// TestMutinyParentTargetedControllerRestrictsToParentsController pins the
// second ParentTargets spelling, ParentTargetedController, on Mutiny's real
// MutinyDamage SVar. Its parent (DBPump) targets a creature an opponent
// controls; the sub-ability's "another target creature that player controls"
// must offer only creatures controlled by the parent target's controller.
func TestMutinyParentTargetedControllerRestrictsToParentsController(t *testing.T) {
	reg := freshCorpusRegistry(t, "m/mutiny.txt")
	mutiny := mustCorpusCard(t, reg, "Mutiny")
	if len(mutiny.Faces) == 0 || len(mutiny.Faces[0].Abilities) == 0 || mutiny.Faces[0].Abilities[0].Sub == nil {
		t.Fatal("precondition: Mutiny has no sub-ability")
	}
	sub := mutiny.Faces[0].Abilities[0].Sub
	if sub.API != "DealDamage" || sub.Params["TargetsWithDefinedController"] != "ParentTargetedController" || sub.Params["ValidTgts"] != "Creature" {
		t.Fatalf("precondition: MutinyDamage SVar = %+v; want DealDamage targeting a Creature with ParentTargetedController", sub)
	}

	e := newSeats(t, 2)
	source := e.G.AddObject(mutiny, 0).ID
	seatZero := onBoardCard(t, e, 0, card(t, "Name:Seat Zero Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"))
	seatOne := onBoardCard(t, e, 1, card(t, "Name:Seat One Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"))
	for _, id := range []state.ObjID{seatZero, seatOne} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: creature %d is not on the battlefield", id)
		}
	}
	if c0, c1 := e.G.Obj(seatZero).Controller, e.G.Obj(seatOne).Controller; c0 == c1 || c0 != 0 || c1 != 1 {
		t.Fatalf("precondition: creature controllers = %d and %d; want distinct seats 0 and 1", c0, c1)
	}

	// Parent DBPump targeted seat 1's creature, so only seat 1's creatures
	// may be offered for the "that player controls" sub-ability.
	parent := []state.Target{{Obj: seatOne}}
	if parent[0].IsPlayer || e.G.Obj(parent[0].Obj).Controller != 1 {
		t.Fatalf("precondition: parent target = %+v; want an object controlled by seat 1", parent[0])
	}
	got := e.LegalSubTargets(0, source, sub, parent)
	if len(got) != 1 || got[0].Obj != seatOne {
		t.Fatalf("ParentTargetedController offer = %+v; want only seat 1's creature %d", got, seatOne)
	}

	// The symmetric parent (seat 0's creature) offers only seat 0's creature.
	parent = []state.Target{{Obj: seatZero}}
	got = e.LegalSubTargets(0, source, sub, parent)
	if len(got) != 1 || got[0].Obj != seatZero {
		t.Fatalf("ParentTargetedController offer = %+v; want only seat 0's creature %d", got, seatZero)
	}
}
