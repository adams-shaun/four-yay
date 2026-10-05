package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func excessTestPermanent(t *testing.T, e *Engine, src string, p state.PlayerID) state.ObjID {
	t.Helper()
	c := card(t, src)
	o := e.G.AddObject(c, p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

func TestExcessDamageHistory(t *testing.T) {
	e := layerEngine(t)
	creature := excessTestPermanent(t, e, "Name:Four Four\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	obj := e.G.Obj(creature)
	if obj.Zone != state.ZBattlefield || e.Toughness(creature) != 4 {
		t.Fatalf("precondition creature zone/toughness: %v/%d", obj.Zone, e.Toughness(creature))
	}
	// Exactly lethal is not excess.
	e.emit(events.Event{Kind: events.Damage, Obj: creature, Amount: 4})
	if obj.WasDealtExcessDamageThisTurn {
		t.Fatal("exactly lethal damage recorded as excess")
	}

	prior := excessTestPermanent(t, e, "Name:Prior Damage\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	priorObj := e.G.Obj(prior)
	e.emit(events.Event{Kind: events.Damage, Obj: prior, Amount: 2})
	if priorObj.Damage != 2 || priorObj.Zone != state.ZBattlefield {
		t.Fatalf("precondition prior marked damage: zone=%v damage=%d", priorObj.Zone, priorObj.Damage)
	}
	// Two marked damage leaves a lethal threshold of two; three more is excess.
	e.emit(events.Event{Kind: events.Damage, Obj: prior, Amount: 3})
	if !priorObj.WasDealtExcessDamageThisTurn {
		t.Fatal("damage above (toughness - prior damage) was not recorded")
	}

	walker := excessTestPermanent(t, e, "Name:Walker\nTypes:Planeswalker Test\nLoyalty:3\nOracle:x\n", 1)
	two := e.G.Obj(walker)
	if two.Zone != state.ZBattlefield || two.Counter("LOYALTY") != 3 {
		t.Fatalf("precondition planeswalker: zone=%v loyalty=%d", two.Zone, two.Counter("LOYALTY"))
	}
	e.emit(events.Event{Kind: events.Damage, Obj: walker, Amount: 4})
	if !two.WasDealtExcessDamageThisTurn {
		t.Fatal("damage above planeswalker loyalty was not recorded")
	}

	below := excessTestPermanent(t, e, "Name:Walker Below\nTypes:Planeswalker Test\nLoyalty:3\nOracle:x\n", 1)
	belowObj := e.G.Obj(below)
	e.emit(events.Event{Kind: events.Damage, Obj: below, Amount: 3})
	if belowObj.WasDealtExcessDamageThisTurn {
		t.Fatal("damage equal to planeswalker loyalty recorded as excess")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0})
	if priorObj.WasDealtExcessDamageThisTurn || two.WasDealtExcessDamageThisTurn {
		t.Fatal("excess-damage history survived TurnChange")
	}
}

func TestRithExcessDamageToken(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	rith := mustCorpusCard(t, reg, "Rith, Liberated Primeval")
	victim := card(t, "Name:Opponent Beast\nTypes:Creature Beast\nPT:4/4\nK:Indestructible\nOracle:x\n")
	e, cfg := tokenReplGameSeats(t, 818, []*cards.Card{rith}, []*cards.Card{victim})
	rithID := moveSeededCard(t, e, 0, rith, state.ZBattlefield)
	victimID := moveSeededCard(t, e, 1, victim, state.ZBattlefield)
	if e.G.Obj(rithID).Zone != state.ZBattlefield || e.G.Obj(victimID).Zone != state.ZBattlefield || e.Toughness(victimID) != 4 {
		t.Fatalf("precondition: Rith=%v victim=%v toughness=%d", e.G.Obj(rithID).Zone, e.G.Obj(victimID).Zone, e.Toughness(victimID))
	}
	e.emit(events.Event{Kind: events.Damage, Obj: victimID, Amount: 5})
	if !e.G.Obj(victimID).WasDealtExcessDamageThisTurn {
		t.Fatal("precondition: opponent creature did not record excess damage")
	}
	if len(rith.Faces[0].Triggers) == 0 {
		t.Fatalf("fixture Rith has no parsed triggers")
	}
	e.setStep(state.StepEnd)
	e.finishEnteredStep()
	if !e.phaseGate(rith.Faces[0].Triggers[0]) {
		t.Fatal("fixture did not reach Rith's end step")
	}
	// Queue the real parsed corpus trigger after proving the damage predicate
	// on its opponent-controlled battlefield recipient. This isolates the
	// trigger body/DragonCheck path from phase queue timing in this fixture.
	e.pushTrigger(pendingTrigger{Source: rithID, Controller: 0, Idx: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("Rith's parsed trigger body was not put on the stack")
	}
	e.resolveTop()
	if n := countTokensNamedOnSeat(t, e, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Rith created %d Dragon tokens, want 1", n)
	}
	replayCheck(t, e, cfg)
}
