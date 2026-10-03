package rules

// PlayerCountOpponents$HasPropertywasDealtNonCombatDamageThisTurn and
// ...LastTurn: the number of opponents who were dealt NONCOMBAT damage this
// turn / during the previous turn (Forge's player property). Readers: Grim
// Repriser's graveyard return ("Activate only if an opponent has been dealt
// noncombat damage this turn"), Whiplash Wordsmith's "As long as an opponent
// was dealt noncombat damage this turn, this creature has flying and haste"
// and Command the Stage's "At the beginning of each upkeep, if an opponent
// was dealt noncombat damage last turn, return this card from your graveyard
// to your hand". Combat damage never qualifies, and damage to yourself never
// counts as an opponent's.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// burn resolves "deals n damage to <defined>" from source as ordinary
// (noncombat) spell/ability damage.
func burn(e *Engine, source state.ObjID, defined string, n int) {
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: e.G.Obj(source).Controller},
		dealDamage(defined, n))
}

// passToNextTurn passes priority (first option on any other ask) until the
// turn number advances.
func passToNextTurn(t *testing.T, e *Engine) {
	t.Helper()
	turn := e.G.Turn
	for range 400 {
		if e.G.Turn != turn {
			return
		}
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while passing the turn")
		}
		if d.Kind == decision.KPriority {
			submitChoices(t, e, passIndex(t, d))
			continue
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	t.Fatal("the turn never ended")
}

func TestNoncombatDamageLedgerSeparatesCombatAndTurns(t *testing.T) {
	t.Parallel()
	e, ids := pcdrEngine(t, "Grizzly Bears")
	bears := ids["Grizzly Bears"]
	if e.WasDealtNoncombatDamageThisTurn(1) || e.WasDealtNoncombatDamageLastTurn(1) {
		t.Fatal("precondition: seat 1 already reads as dealt noncombat damage")
	}
	// Combat damage is not noncombat damage.
	pcdrCombat(e, bears, 1, 2)
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("precondition: combat hit did not land (life %d)", got)
	}
	if e.WasDealtNoncombatDamageThisTurn(1) {
		t.Fatal("combat damage counted as noncombat damage")
	}
	// Spell/ability damage is.
	burn(e, bears, "Opponent", 1)
	if !e.WasDealtNoncombatDamageThisTurn(1) {
		t.Fatal("ability damage to seat 1 not recorded as noncombat damage")
	}
	if e.WasDealtNoncombatDamageThisTurn(0) {
		t.Fatal("seat 0 recorded as damaged though it was not dealt damage")
	}
	e.priorityRound() // pcdrEngine clears the pending ask
	passToNextTurn(t, e)
	if e.WasDealtNoncombatDamageThisTurn(1) {
		t.Fatal("this-turn record survived the turn change")
	}
	if !e.WasDealtNoncombatDamageLastTurn(1) {
		t.Fatal("last-turn record missing on the following turn")
	}
	passToNextTurn(t, e)
	if e.WasDealtNoncombatDamageLastTurn(1) {
		t.Fatal("last-turn record survived two turn changes")
	}
}

func TestGrimRepriserNeedsAnOpponentDealtNoncombatDamage(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{
		lookup(t, reg, "Grim Repriser"),
		lookup(t, reg, "Grizzly Bears"),
	}, nil)
	rep := moveByName(t, e, 0, "Grim Repriser", state.ZGraveyard)
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)

	addMana(t, e, 0, "BR")
	if _, ok := findAbilityOption(e, rep, 0); ok {
		t.Fatal("Grim Repriser's return is offered with no damage dealt")
	}
	// Damage to its own controller is not damage to an opponent.
	burn(e, bears, "You", 1)
	e.priorityRound()
	if _, ok := findAbilityOption(e, rep, 0); ok {
		t.Fatal("Grim Repriser's return is offered after damage to its controller only")
	}
	burn(e, bears, "Opponent", 1)
	e.priorityRound()
	opt, ok := findAbilityOption(e, rep, 0)
	if !ok {
		t.Fatalf("Grim Repriser's return is not offered after noncombat damage to an opponent: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(rep)
	if o == nil || o.Zone != state.ZBattlefield || o.Counter("FINALITY") != 1 {
		t.Fatalf("Grim Repriser did not return with a finality counter: %+v", o)
	}
	replayCheck(t, e, cfg)
}

func TestWhiplashWordsmithFliesAfterNoncombatDamageToOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{lookup(t, reg, "Whiplash Wordsmith")}, nil)
	ws := moveByName(t, e, 0, "Whiplash Wordsmith", state.ZBattlefield)
	if e.HasKeyword(ws, "Flying") || e.HasKeyword(ws, "Haste") {
		t.Fatal("Whiplash Wordsmith has flying/haste with no damage dealt")
	}
	burn(e, ws, "You", 2)
	if e.HasKeyword(ws, "Flying") {
		t.Fatal("damage to its controller gave Whiplash Wordsmith flying")
	}
	burn(e, ws, "Opponent", 1)
	if !e.HasKeyword(ws, "Flying") || !e.HasKeyword(ws, "Haste") {
		t.Fatal("Whiplash Wordsmith lacks flying/haste after noncombat damage to an opponent")
	}
	passToNextTurn(t, e)
	if e.HasKeyword(ws, "Flying") {
		t.Fatal("Whiplash Wordsmith kept flying into the next turn")
	}
}

func TestCommandTheStageReturnsAfterNoncombatDamageLastTurn(t *testing.T) {
	t.Parallel()
	for _, damaged := range []bool{false, true} {
		reg := testutil.CorpusRegistry(t)
		e, cfg := corpusEngineCfg(t, reg, []*cards.Card{
			lookup(t, reg, "Command the Stage"),
			lookup(t, reg, "Grizzly Bears"),
		}, nil)
		cts := moveByName(t, e, 0, "Command the Stage", state.ZGraveyard)
		bears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
		e.priorityRound()
		if damaged {
			burn(e, bears, "Opponent", 1)
		}
		// Seat 1's upkeep: "last turn" is seat 0's turn just ended.
		driveToTurn(t, e, e.G.Turn+1, 1)
		got := e.G.Obj(cts).Zone
		if damaged && got != state.ZHand {
			t.Fatalf("Command the Stage stayed in %v though an opponent was dealt noncombat damage last turn", got)
		}
		if !damaged && got != state.ZGraveyard {
			t.Fatalf("Command the Stage moved to %v with no damage dealt last turn", got)
		}
		replayCheck(t, e, cfg)
	}
}
