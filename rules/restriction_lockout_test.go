package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The CantBeCast / CantBeActivated condition and SVar gates (rules/statics.go
// restrictionGateHolds / checkSVarHolds on the restriction paths, plus the
// hand-live self-restriction merge castRestrictionSources): the corpus's
// condition-scoped lockouts -- Grand Abolisher's Condition$ PlayerTurn +
// AffectedZone$ Battlefield pair, and the SVar-gated self-restrictions Rakdos
// Lord of Riots and Serra Avenger carry on themselves with EffectZone$ All.
// The census labels these tests retire are param:stat:CantBeActivated.AffectedZone,
// param:stat:CantBeActivated.Condition, param:stat:CantBeCast.Condition and
// param:stat:CantBeCast.CheckSVar/SVarCompare (rules/paramcensus_test.go's
// knownUnsupportedParams).

// castOptionNamed finds the "cast" option for id in the current priority
// decision, or nil.
func castOptionNamed(e *Engine, id state.ObjID) *decision.Option {
	d := e.Pending()
	if d == nil {
		return nil
	}
	for i := range d.Options {
		if d.Options[i].Kind == "cast" && d.Options[i].Obj == id {
			return &d.Options[i]
		}
	}
	return nil
}

// TestGrandAbolisherLocksOpponentActionsToTheAbolisherTurn drives the real
// corpus card: Condition$ PlayerTurn resolves against the SOURCE's
// controller (the abolisher's turn, never the restricted caster's), and
// AffectedZone$ Battlefield admits a battlefield permanent's abilities.
func TestGrandAbolisherLocksOpponentActionsToTheAbolisherTurn(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	onBoardCard(t, e, 0, corpusCard(t, "Grand Abolisher"))
	onBoardCard(t, e, 1, card(t, "Name:Gear\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ GainLife | Cost$ 1 | Defined$ You | LifeAmount$ 1 | SpellDescription$ x\nOracle:x\n"))
	e.G.Players[1].Pool[state.MC] = 1

	// Seat 0's turn (handEngine lands on seat 0's Main1): the opponent's
	// artifact activation is locked out.
	if n := kinds(e.legalActions(1))["ability"]; n != 0 {
		t.Fatalf("opponent artifact activations offered on the abolisher's turn: %d", n)
	}
	// Seat 1's own turn: the condition no longer holds, the activation is
	// offered again.
	e.G.Active = 1
	if n := kinds(e.legalActions(1))["ability"]; n != 1 {
		t.Fatalf("artifact activations offered on the opponent's own turn: %d, want 1", n)
	}
}

// TestRakdosSVarCastLockoutFollowsOpponentLifeLoss drives the real corpus
// card's self-restriction: the static is live from the HAND (EffectZone$ All,
// castRestrictionSources), and CheckSVar$ X / SVarCompare$ EQ0 gates it on
// Count$LifeOppsLostThisTurn (rules/stack.go LifeLostThisTurn).
func TestRakdosSVarCastLockoutFollowsOpponentLifeLoss(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Rakdos, Lord of Riots"))
	rakdos := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "BBRR")
	if n := kinds(e.legalActions(0))["cast"]; n != 0 {
		t.Fatalf("Rakdos offered while no opponent has lost life this turn (X=0, EQ0 holds): %d", n)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -2})
	e.priorityRound()
	opt := castOptionNamed(e, rakdos)
	if opt == nil {
		t.Fatal("Rakdos still locked after an opponent lost life (X=2, EQ0 fails)")
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 50)
	if o := e.G.Obj(rakdos); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Rakdos zone=%v, want battlefield", o)
	}
}

// TestSerraAvengerSVarLocksHerFirstThreeTurns drives the real corpus card's
// other SVar shape: Count$YourTurns (effects/count.go through the new
// log-derived Host.TurnsTaken) counts the turns that have begun with the
// caster as active, so LE3 locks her first, second and third turns and the
// fourth turn unlocks her (Caster$ Player.Active scopes the lockout to her
// own turns, which is the only window a creature cast has anyway).
func TestSerraAvengerSVarLocksHerFirstThreeTurns(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Serra Avenger"))
	serra := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MW] = 2
	castSerra := func() int {
		n := 0
		for _, o := range e.legalActions(0) {
			if o.Kind == "cast" && o.Obj == serra {
				n++
			}
		}
		return n
	}
	if n := castSerra(); n != 0 {
		t.Fatalf("Serra offered on turn 1 (TurnsTaken=1, LE3 holds): %d", n)
	}
	// Drive the turn structure forward with real TurnChange events (each one
	// a turn that BEGINS -- what TurnsTaken folds): seat 0's third turn still
	// locks her, her fourth turn unlocks her.
	emitTurn := func(p state.PlayerID, n int32) {
		e.emit(events.Event{Kind: events.TurnChange, Player: p, Amount: n})
	}
	emitTurn(0, 1)
	emitTurn(1, 2)
	emitTurn(0, 3)
	emitTurn(1, 4)
	emitTurn(0, 5)
	if e.G.Active != 0 || e.G.Turn != 5 {
		t.Fatalf("turn structure = %d/%d, want 5/seat 0", e.G.Turn, e.G.Active)
	}
	e.G.Players[0].Pool[state.MW] = 2
	if n := castSerra(); n != 0 {
		t.Fatalf("Serra offered on her third turn (TurnsTaken=3, LE3 holds): %d", n)
	}
	emitTurn(1, 6)
	emitTurn(0, 7)
	e.G.Players[0].Pool[state.MW] = 2
	if n := castSerra(); n != 1 {
		t.Fatalf("Serra cast options on her fourth turn: %d, want 1", n)
	}
}

// TestSelfCastLockoutConditionResolvesAgainstTheSourceTurn pins the gate on
// the synthetic self-restriction shape: Condition$ PlayerTurn on a card's own
// CantBeCast static is the CARD controller's turn, and the self-merge only
// lifts a hand static whose EffectZone$ says it is live there (a static with
// Forge's default battlefield EffectZone stays dead from the hand).
func TestSelfCastLockoutConditionResolvesAgainstTheSourceTurn(t *testing.T) {
	t.Parallel()
	live := "Name:Selflock\nManaCost:1\nTypes:Instant\n" +
		"S:Mode$ CantBeCast | ValidCard$ Card.Self | Condition$ PlayerTurn | EffectZone$ All\nOracle:x\n"
	dead := "Name:BattlefieldOnly\nManaCost:1\nTypes:Instant\n" +
		"S:Mode$ CantBeCast | ValidCard$ Card.Self\nOracle:x\n"
	e := handEngine(t, card(t, live), card(t, dead))
	liveID := e.G.Zone(state.ZHand, 0)[0]
	deadID := e.G.Zone(state.ZHand, 0)[1]
	e.G.Players[0].Pool[state.MC] = 2

	// On the controller's turn only the unconditioned card is offered; the
	// EffectZone$-live self-lockout withholds its own cast.
	if n := kinds(e.legalActions(0))["cast"]; n != 1 {
		t.Fatalf("casts offered on the controller's turn: %d, want 1", n)
	}
	// On the opponent's turn the condition fails and both are offered.
	e.G.Active = 1
	if n := kinds(e.legalActions(0))["cast"]; n != 2 {
		t.Fatalf("hand casts offered on the opponent's turn: %d, want 2", n)
	}
	if liveID == deadID {
		t.Fatal("fixture setup")
	}
}
