package rules

// The attackedThisTurn filter predicate: "target creature that attacked this
// turn" (Hexhaven Dueling Arena) and the 39 other corpus cards that read it
// used to evaluate as an unknown word and fail closed. The state it reads is
// the DeclareAttackers fold's stamp, so every transition below goes through
// e.emit, never a field write.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestAttackedThisTurnIsARecognisedPredicate(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"Creature.attackedThisTurn", "Creature.!attackedThisTurn", "Card.Self+attackedThisTurn",
		"Creature.YouCtrl+!ThisTurnEntered+!attackedThisTurn", "Card.EnchantedBy+!attackedThisTurn",
	} {
		if un := effects.UnknownPredicates(spec); len(un) != 0 {
			t.Errorf("UnknownPredicates(%q) = %v, want none", spec, un)
		}
	}
	// blockedThisTurn has no stamp and stays unknown, so a mixed form keeps
	// failing closed through that word.
	if un := effects.UnknownPredicates("Creature.attackedThisTurn,Creature.blockedThisTurn"); len(un) != 1 || un[0] != "blockedThisTurn" {
		t.Errorf("UnknownPredicates(mixed form) = %v, want [blockedThisTurn]", un)
	}
}

// TestAttackedThisTurnFollowsTheAttackStamp walks one attacker and one bystander
// through the declaration, a later combat phase, the turn boundary and a zone
// change.
func TestAttackedThisTurnFollowsTheAttackStamp(t *testing.T) {
	t.Parallel()
	const bear = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := combatEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	atk := onBoardReady(t, e, 0, bear)
	idle := onBoardReady(t, e, 0, bear)
	if e.G.Obj(atk).Zone != state.ZBattlefield || e.G.Obj(idle).Zone != state.ZBattlefield {
		t.Fatal("precondition: both creatures are on the battlefield")
	}
	did := func(id state.ObjID) bool {
		return effects.MatchesObjectCtx(e.G, "Creature.attackedThisTurn", e.G.Obj(id), effects.SpecContext{})
	}
	didNot := func(id state.ObjID) bool {
		return effects.MatchesObjectCtx(e.G, "Creature.!attackedThisTurn", e.G.Obj(id), effects.SpecContext{})
	}
	// Before the attack the two spellings must disagree, or the rest proves nothing.
	if did(atk) || !didNot(atk) {
		t.Fatalf("before attacking: attackedThisTurn=%v !attackedThisTurn=%v, want false/true", did(atk), didNot(atk))
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{atk}})
	if !did(atk) || didNot(atk) {
		t.Fatalf("after attacking: attackedThisTurn=%v !attackedThisTurn=%v, want true/false", did(atk), didNot(atk))
	}
	if did(idle) || !didNot(idle) {
		t.Fatal("the creature that did not attack matches attackedThisTurn")
	}
	// The difference from attackedThisCombat: a later combat phase of the same
	// turn keeps it, while the combat-clock predicate has lapsed.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	if effects.MatchesObjectCtx(e.G, "Creature.attackedThisCombat", e.G.Obj(atk), effects.SpecContext{}) {
		t.Fatal("precondition: attackedThisCombat should have lapsed in the later combat phase")
	}
	if !did(atk) {
		t.Fatal("attackedThisTurn lapsed in a later combat phase of the same turn")
	}

	// CR 400.7: a creature that leaves and returns is a new object.
	e.emit(events.Event{Kind: events.MoveZone, Obj: atk, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: atk, From: state.ZHand, To: state.ZBattlefield})
	if e.G.Obj(atk).Zone != state.ZBattlefield {
		t.Fatal("precondition: the creature is back on the battlefield")
	}
	if did(atk) {
		t.Fatal("attackedThisTurn survived the creature leaving and re-entering the battlefield")
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{atk}})
	if !did(atk) {
		t.Fatal("precondition: the creature attacked again")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if did(atk) {
		t.Fatal("attackedThisTurn survived the turn boundary")
	}
}

// TestAttackedThisTurnGivesAgentFrankHorriganIndestructible drives the real
// static ability (Affected$ Card.attackedThisTurn): the keyword turns on through
// the DeclareAttackers event and off again at the turn boundary.
func TestAttackedThisTurnGivesAgentFrankHorriganIndestructible(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Agent Frank Horrigan")
	if !ok {
		t.Fatal("Agent Frank Horrigan missing from the corpus")
	}
	e := combatEngine(t)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	id := onBoardCard(t, e, 0, c)
	e.G.Obj(id).SummonSick = false
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: Agent Frank Horrigan is on the battlefield")
	}
	if e.HasKeyword(id, "Indestructible") {
		t.Fatal("Agent Frank Horrigan is indestructible before attacking")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	if !e.HasKeyword(id, "Indestructible") {
		t.Fatal("Agent Frank Horrigan is not indestructible after attacking")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if e.HasKeyword(id, "Indestructible") {
		t.Fatal("Agent Frank Horrigan is still indestructible in the next turn")
	}
}

// TestAttackedThisTurnGatesErgRaidersEndStepTrigger drives Erg Raiders'
// `IsPresent$ Card.Self+!attackedThisTurn` intervening-if: queued for the
// Raiders that stayed home, not for the one that attacked.
func TestAttackedThisTurnGatesErgRaidersEndStepTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Erg Raiders")
	if !ok {
		t.Fatal("Erg Raiders missing from the corpus")
	}
	queued := func(attack bool) int {
		e := combatEngine(t)
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
		id := onBoardCard(t, e, 0, c)
		e.G.Obj(id).SummonSick = false
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatal("precondition: Erg Raiders is on the battlefield")
		}
		if attack {
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
		}
		return phaseTriggersQueued(e, state.StepEnd)
	}
	if n := queued(false); n != 1 {
		t.Fatalf("Erg Raiders that did not attack queued %d end-step triggers, want 1", n)
	}
	if n := queued(true); n != 0 {
		t.Fatalf("Erg Raiders that attacked queued %d end-step triggers, want 0", n)
	}
}
