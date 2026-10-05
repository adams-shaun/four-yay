package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
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
	killer := excessTestPermanent(t, e, "Name:Touch of Death\nTypes:Creature Assassin\nPT:1/1\nK:Deathtouch\nOracle:x\n", 1)
	if !e.HasKeyword(killer, "Deathtouch") {
		t.Fatal("precondition: damage source lacks deathtouch")
	}
	lethalTouch := excessTestPermanent(t, e, "Name:Untouched Four\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	if e.Toughness(lethalTouch) != 4 || e.G.Obj(lethalTouch).Damage != 0 {
		t.Fatal("precondition: deathtouch recipient is not an undamaged 4/4")
	}
	e.damaging = killer
	e.emit(events.Event{Kind: events.Damage, Obj: lethalTouch, Amount: 2})
	e.damaging = 0
	if !e.G.Obj(lethalTouch).WasDealtExcessDamageThisTurn {
		t.Fatal("two deathtouch damage was not classified as excess over lethal damage")
	}
	// Remaining lethal is zero even with deathtouch when an indestructible
	// creature has already taken lethal marked damage.
	marked := excessTestPermanent(t, e, "Name:Marked Touch Victim\nTypes:Creature Beast\nPT:4/4\nK:Indestructible\nOracle:x\n", 1)
	markedObj := e.G.Obj(marked)
	e.emit(events.Event{Kind: events.Damage, Obj: marked, Amount: 4})
	if markedObj.Zone != state.ZBattlefield || markedObj.Damage != 4 || markedObj.WasDealtExcessDamageThisTurn || e.Toughness(marked) != 4 {
		t.Fatalf("precondition marked 4/4: zone=%v damage=%d excess=%v toughness=%d", markedObj.Zone, markedObj.Damage, markedObj.WasDealtExcessDamageThisTurn, e.Toughness(marked))
	}
	e.damaging = killer
	e.emit(events.Event{Kind: events.Damage, Obj: marked, Amount: 1})
	e.damaging = 0
	if !markedObj.WasDealtExcessDamageThisTurn {
		t.Fatal("deathtouch raised already-zero remaining lethal to one")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0})
	if priorObj.WasDealtExcessDamageThisTurn || two.WasDealtExcessDamageThisTurn || len(e.G.ExcessDamageVictims) != 0 {
		t.Fatal("excess-damage history survived TurnChange")
	}
}

func TestExcessDamageCombatPath(t *testing.T) {
	e := layerEngine(t)
	attacker := excessTestPermanent(t, e, "Name:Attacker\nTypes:Creature Beast\nPT:5/5\nOracle:x\n", 0)
	blocker := excessTestPermanent(t, e, "Name:Blocker\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	if e.G.Obj(attacker).Zone != state.ZBattlefield || e.G.Obj(blocker).Zone != state.ZBattlefield || e.Toughness(blocker) != 4 {
		t.Fatalf("precondition combat permanents: attacker=%v blocker=%v toughness=%d", e.G.Obj(attacker).Zone, e.G.Obj(blocker).Zone, e.Toughness(blocker))
	}
	e.G.Active = 0
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
	e.emit(events.Event{Kind: events.DeclareBlockers, Pairs: [][2]state.ObjID{{attacker, blocker}}})
	e.dealCombatDamage()
	if got := e.G.Obj(blocker).Damage; got != 5 || !e.G.Obj(blocker).WasDealtExcessDamageThisTurn {
		t.Fatalf("combat assignment marked damage=%d excess=%v; want 5 and true", got, e.G.Obj(blocker).WasDealtExcessDamageThisTurn)
	}
}

// TestExcessDamageNoncombatPath drives the real DealDamage primitive through
// its event emitter; excess must use the recipient's remaining toughness.
func TestExcessDamageNoncombatPath(t *testing.T) {
	e := layerEngine(t)
	source := excessTestPermanent(t, e, "Name:Burn Source\nTypes:Creature Wizard\nPT:1/1\nOracle:x\n", 0)
	victim := excessTestPermanent(t, e, "Name:Burn Victim\nTypes:Creature Beast\nPT:4/4\nOracle:x\n", 1)
	o := e.G.Obj(victim)
	if e.G.Obj(source).Zone != state.ZBattlefield || o.Zone != state.ZBattlefield || e.Toughness(victim) != 4 || o.Damage != 0 {
		t.Fatalf("precondition: source=%v victim=%v toughness=%d damage=%d", e.G.Obj(source).Zone, o.Zone, e.Toughness(victim), o.Damage)
	}
	ctx := effects.NewCtxPtr(source, 0, effects.CtxInit{Targets: []state.Target{{Obj: victim}}})
	sa := &cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Targeted", "NumDmg": "2"}}
	effects.Resolve(e, ctx, sa)
	if o.Damage != 2 || o.WasDealtExcessDamageThisTurn {
		t.Fatalf("below lethal: damage=%d excess=%v", o.Damage, o.WasDealtExcessDamageThisTurn)
	}
	// Remaining lethal is two; the next three damage must record excess.
	sa = &cards.SA{Kind: "DB", API: "DealDamage", Params: map[string]string{"Defined": "Targeted", "NumDmg": "3"}}
	effects.Resolve(e, ctx, sa)
	if o.Damage != 5 || !o.WasDealtExcessDamageThisTurn {
		t.Fatalf("above remaining lethal: damage=%d excess=%v", o.Damage, o.WasDealtExcessDamageThisTurn)
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
	ctx := effects.NewCtxPtr(rithID, 0, effects.CtxInit{SVars: rith.Faces[0].SVars})
	if n, ok := effects.EvalCountOK(e, ctx, rith.Faces[0].SVars["DragonCheck"]); !ok || n != 1 {
		t.Fatalf("precondition Rith DragonCheck = %d, %v, want 1, true", n, ok)
	}
	if holds, ok := effects.CheckSVarHolds(e, ctx, "DragonCheck", ""); !ok || !holds {
		t.Fatalf("precondition Rith check = %v, %v", holds, ok)
	}
	// Enter the end step through the ordinary StepChange trigger scan; no
	// synthetic pendingTrigger is injected. Clear setup-time decisions before
	// the event whose normal trigger match is under test.
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if !e.phaseGate(rith.Faces[0].Triggers[0]) || e.G.Active != 0 {
		t.Fatalf("precondition: end step/active seat = %v/%d", e.G.Step, e.G.Active)
	}
	e.priorityRound()
	if n := crTriggerStackCount(e, rithID); n != 1 {
		t.Fatalf("Rith's real end-step trigger queued %d stack objects, want 1", n)
	}
	passUntilStackEmpty(t, e, 20)
	if n := countTokensNamedOnSeat(t, e, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Rith created %d Dragon tokens, want 1", n)
	}
	replayCheck(t, e, cfg)

	// The same printed CheckSVar$ must see a planeswalker as well: loyalty,
	// not toughness, sets its lethal threshold. Exactly lethal stays false.
	walker := card(t, "Name:Opponent Walker\nTypes:Planeswalker Test\nLoyalty:3\nOracle:x\n")
	w, wc := tokenReplGameSeats(t, 819, []*cards.Card{rith}, []*cards.Card{walker})
	wr := moveSeededCard(t, w, 0, rith, state.ZBattlefield)
	wid := moveSeededCard(t, w, 1, walker, state.ZBattlefield)
	wo := w.G.Obj(wid)
	if w.G.Obj(wr).Zone != state.ZBattlefield || wo.Zone != state.ZBattlefield || wo.Counter("LOYALTY") != 3 {
		t.Fatalf("precondition walker/Rith zones or loyalty: %v/%v/%d", w.G.Obj(wr).Zone, wo.Zone, wo.Counter("LOYALTY"))
	}
	w.emit(events.Event{Kind: events.Damage, Obj: wid, Amount: 3})
	if wo.WasDealtExcessDamageThisTurn || wo.Counter("LOYALTY") != 0 {
		t.Fatalf("exactly lethal walker hit: excess=%v loyalty=%d", wo.WasDealtExcessDamageThisTurn, wo.Counter("LOYALTY"))
	}
	walkerCtx := effects.NewCtxPtr(wr, 0, effects.CtxInit{SVars: rith.Faces[0].SVars})
	if n, ok := effects.EvalCountOK(w, walkerCtx, rith.Faces[0].SVars["DragonCheck"]); !ok || n != 0 {
		t.Fatalf("exactly lethal walker counted: %d, %v", n, ok)
	}
	w.emit(events.Event{Kind: events.Damage, Obj: wid, Amount: 1})
	if !wo.WasDealtExcessDamageThisTurn {
		t.Fatal("precondition: one above zero loyalty must record excess")
	}
	w.pending = nil
	w.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	w.checkStateBased()
	w.priorityRound()
	if wo.Zone != state.ZGraveyard {
		t.Fatalf("precondition: excess-damaged walker must have died before Rith resolves, zone=%v", wo.Zone)
	}
	if n := crTriggerStackCount(w, wr); n != 1 {
		t.Fatalf("Rith's planeswalker excess trigger queued %d, want 1", n)
	}
	passUntilStackEmpty(t, w, 20)
	if n := countTokensNamedOnSeat(t, w, 0, "Dragon Token"); n != 1 {
		t.Fatalf("Rith's planeswalker excess created %d Dragons, want 1", n)
	}
	replayCheck(t, w, wc)
}
