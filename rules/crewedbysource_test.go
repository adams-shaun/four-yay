package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// crewedBySourceStackAbility reports a triggered ability on the stack whose
// source is src.
func crewedBySourceStackAbility(e *Engine, src state.ObjID) state.ObjID {
	for _, id := range e.G.Stack {
		o := e.G.Obj(id)
		if o != nil && o.Source == src && state.StackKindOf(e.G, o) == state.StackKindTriggered {
			return id
		}
	}
	return 0
}

// answerCrewTapWith elects option in the pending crew tap ask, if one names
// it, and drains the stack afterwards. It mirrors crewVehicle's flow
// (crewedthisturn_test.go) so the two cannot drift apart.
func answerCrewTapWith(t *testing.T, e *Engine, crewer state.ObjID) {
	t.Helper()
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		chosen := -1
		for _, o := range d.Options {
			if o.Obj == crewer {
				chosen = o.Index
			}
		}
		if chosen < 0 {
			break
		}
		submitChoices(t, e, chosen)
	}
	passUntilStackEmpty(t, e, 40)
}

// crewWith drives seat 0's priority window to van's Crew ability and pays it
// by tapping crewer.
func crewWith(t *testing.T, e *Engine, van, crewer state.ObjID) {
	t.Helper()
	if e.Pending() == nil {
		e.priorityRound()
	}
	opt := abilityFor(t, e, 0, van)
	if opt == nil {
		t.Fatalf("precondition: the Vehicle's Crew ability not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	answerCrewTapWith(t, e, crewer)
}

// crewedBySourceScenario builds a p0-first game with Balthier, a bear and a
// Vehicle on the battlefield, and returns the three ids.
func crewedBySourceScenario(t *testing.T, seed uint64) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	balthier := setAuditRealCard(t, "Balthier and Fran")
	vehicle := setAuditRealCard(t, "Unicycle") // haste: a setup cannot place it, so CR 302.6 sickness must not hold it back
	bear := card(t, ninjutsuBearSrc)
	e, _ := setAuditDeck(t, seed, []*cards.Card{balthier, vehicle, bear}, nil)
	balthierID := searchMoveByName(t, e, "Balthier and Fran", state.ZBattlefield)
	bearID := putCreature(t, e, 0, ninjutsuBearSrc)
	vehicleID := searchMoveByName(t, e, "Unicycle", state.ZBattlefield)
	for _, id := range []state.ObjID{balthierID, bearID, vehicleID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: fixture %d not on the battlefield: %+v", id, o)
		}
	}
	return e, balthierID, bearID, vehicleID
}

// TestCrewedBySourceThisTurnAttacksTrigger pins the reverse-direction crew
// predicate (Forge's Vehicle.CrewedBySourceThisTurn, effects/filter.go):
// Balthier and Fran's "Whenever a Vehicle crewed by CARDNAME this turn
// attacks, if it's the first combat phase of the turn" fires when the SOURCE
// crewed the attacking Vehicle this turn, and does not fire when another
// creature crewed it.
func TestCrewedBySourceThisTurnAttacksTrigger(t *testing.T) {
	e, balthierID, _, vehicleID := crewedBySourceScenario(t, 8241)
	crewWith(t, e, vehicleID, balthierID)
	if o := e.G.Obj(vehicleID); o == nil || !e.IsCreature(vehicleID) {
		t.Fatalf("precondition: Unicycle did not become a creature after crewing: %+v", o)
	}
	if balthier := e.G.Obj(balthierID); balthier == nil || !balthier.Tapped {
		t.Fatalf("precondition: the crew did not tap Balthier: %+v", balthier)
	}
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, vehicleID)
	if crewedBySourceStackAbility(e, balthierID) == 0 {
		t.Fatalf("Balthier's crew-by-source attack trigger never reached the stack: %+v", e.Pending())
	}
}

// TestCrewedBySourceThisTurnOtherCrewerDoesNotFire is the negative control:
// the Vehicle crewed by ANOTHER creature must not fire the trigger.
func TestCrewedBySourceThisTurnOtherCrewerDoesNotFire(t *testing.T) {
	e, balthierID, bearID, vehicleID := crewedBySourceScenario(t, 8242)
	crewWith(t, e, vehicleID, bearID)
	if o := e.G.Obj(vehicleID); o == nil || !e.IsCreature(vehicleID) {
		t.Fatalf("precondition: Unicycle did not become a creature after crewing: %+v", o)
	}
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, vehicleID)
	if ab := crewedBySourceStackAbility(e, balthierID); ab != 0 {
		t.Fatalf("Balthier's trigger fired on a Vehicle another creature crewed: %d", ab)
	}
}
