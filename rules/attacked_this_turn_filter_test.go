package rules

// The attackedThisTurn filter predicate: effects.UnknownPredicates used to
// report it, so every condition that read it failed closed (Erg Raiders'
// end-step gate, Full Throttle's UntapAll, Kratos's count). The tests drive
// the REAL Erg Raiders through events only; the state the predicate reads
// moves through the event fold, never by writing the stamp fields.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestAttackedThisTurnIsNoLongerUnknown(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"Card.Self+attackedThisTurn", "Card.Self+!attackedThisTurn",
		"Creature.!attackedThisTurn", "Creature.YouCtrl+attackedThisTurn",
		"Creature.!attackedThisTurn+ActivePlayerCtrl",
	} {
		if un := effects.UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want none", spec, un)
		}
	}
}

// ergRaidersOnBoard puts the real Erg Raiders on seat 0's battlefield having
// been under control since before the turn (summoning sickness cleared).
func ergRaidersOnBoard(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := combatEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	id := onBoardCard(t, e, 0, corpusCard(t, "Erg Raiders"))
	e.G.Obj(id).SummonSick = false
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: Erg Raiders is on the battlefield")
	}
	if e.G.CombatsThisTurn < 1 {
		t.Fatalf("precondition: the combat clock must have started (CombatsThisTurn = %d)", e.G.CombatsThisTurn)
	}
	return e, id
}

func attackedThisTurnMatch(e *Engine, id state.ObjID, spec string) bool {
	return effects.MatchesObjectCtx(e.G, spec, e.G.Obj(id), effects.SpecContext{Source: id})
}

// TestAttackedThisTurnTruthTable: false before attacking, true after, still
// true in a later (extra) combat where attackedThisCombat has lapsed, and
// false again after the turn boundary. The negated form is the exact inverse.
func TestAttackedThisTurnTruthTable(t *testing.T) {
	t.Parallel()
	e, id := ergRaidersOnBoard(t)
	const pos, neg, combat = "Card.Self+attackedThisTurn", "Card.Self+!attackedThisTurn", "Card.Self+attackedThisCombat"
	check := func(when string, wantPos, wantCombat bool) {
		t.Helper()
		if got := attackedThisTurnMatch(e, id, pos); got != wantPos {
			t.Fatalf("%s: attackedThisTurn = %v, want %v", when, got, wantPos)
		}
		if got := attackedThisTurnMatch(e, id, neg); got != !wantPos {
			t.Fatalf("%s: !attackedThisTurn = %v, want %v", when, got, !wantPos)
		}
		if got := attackedThisTurnMatch(e, id, combat); got != wantCombat {
			t.Fatalf("%s: attackedThisCombat = %v, want %v", when, got, wantCombat)
		}
	}
	check("before attacking", false, false)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	check("after declaring the attack", true, true)

	// A second combat phase this turn: the combat clock moves on, the turn
	// stamp does not. This is what separates the two predicates.
	before := e.G.CombatsThisTurn
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	if e.G.CombatsThisTurn == before {
		t.Fatalf("precondition: BeginCombat did not advance the combat clock (%d)", before)
	}
	check("in a later combat of the same turn", true, false)

	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	check("after the turn boundary", false, false)
}

// TestAttackedThisTurnDoesNotSurviveBlink: CR 400.7. A creature that attacked
// and then left and re-entered is a new object that has not attacked this
// turn. AttacksThisTurn survives the zone change, so an implementation
// reading it would wrongly stay true; the precondition pins that.
func TestAttackedThisTurnDoesNotSurviveBlink(t *testing.T) {
	t.Parallel()
	e, id := ergRaidersOnBoard(t)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	if !attackedThisTurnMatch(e, id, "Card.Self+attackedThisTurn") {
		t.Fatal("precondition: Erg Raiders attacked this turn")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, Player: 0, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, Player: 0, From: state.ZGraveyard, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Erg Raiders re-entered the battlefield (zone %v)", o.Zone)
	}
	if o.AttacksThisTurn == 0 {
		t.Fatal("precondition: AttacksThisTurn survives the blink, so it cannot be the predicate's source")
	}
	if attackedThisTurnMatch(e, id, "Card.Self+attackedThisTurn") ||
		!attackedThisTurnMatch(e, id, "Card.Self+!attackedThisTurn") {
		t.Fatal("a creature that left and re-entered still reads as having attacked this turn")
	}
}

// TestAttackedThisTurnGatesErgRaidersEndStepTrigger: "At the beginning of
// your end step, if Erg Raiders didn't attack this turn, it deals 2 damage to
// you unless it came under your control this turn." The intervening-if reads
// Card.Self+!attackedThisTurn.
func TestAttackedThisTurnGatesErgRaidersEndStepTrigger(t *testing.T) {
	t.Parallel()

	// Did not attack: the trigger queues and resolves into 2 damage.
	e, id := ergRaidersOnBoard(t)
	if !attackedThisTurnMatch(e, id, "Card.Self+!attackedThisTurn") {
		t.Fatal("precondition: Erg Raiders has not attacked")
	}
	life := e.G.Players[0].Life
	if n := stepTriggers(e, state.StepEnd, 0); n != 1 {
		t.Fatalf("end step queued %d triggers for a non-attacking Erg Raiders, want 1", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life-2 {
		t.Fatalf("life after the trigger = %d, want %d", got, life-2)
	}

	// Attacked: the intervening-if is false, nothing queues.
	e, id = ergRaidersOnBoard(t)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	if !attackedThisTurnMatch(e, id, "Card.Self+attackedThisTurn") {
		t.Fatal("precondition: Erg Raiders attacked")
	}
	if n := stepTriggers(e, state.StepEnd, 0); n != 0 {
		t.Fatalf("end step queued %d triggers for an attacking Erg Raiders, want 0", n)
	}
	for _, n := range notesOf(e) {
		if strings.Contains(n, "unimplemented") || strings.Contains(n, "unknown") {
			t.Fatalf("engine note reports an unsupported shape: %q", n)
		}
	}
}
