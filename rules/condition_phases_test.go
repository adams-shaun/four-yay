package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// condition_phases_test.go pins the ConditionPlayerTurn$ / ConditionPhases$
// condition keys on the shared Condition* gate (effects.conditionMet), the
// Unbreakable Formation task agent-20260918T200326Z-10b49320: the Addendum
// family (ConditionPlayerTurn$ True | ConditionPhases$ Main1,Main2 |
// ConditionDefined$ Self | ConditionPresent$ Card.wasCast) used to report
// the whole gate unsupported and run unconditionally on EVERY cast.
//
// The Unbreakable Formation / Eddymurk Crab tests run the REAL corpus cards
// (searchTestRegistry) — no Forge script text is committed; the small
// fixture cards are authored inline.

const condPhasesBearSrc = "Name:Probe Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

const condPhasesFailOpenSrc = "Name:Phase Probe\nManaCost:W\nTypes:Instant\n" +
	"A:SP$ PumpAll | ValidCards$ Creature.YouCtrl | KW$ Indestructible | SubAbility$ DBProbe | SpellDescription$ indestructible, then the probe.\n" +
	"SVar:DBProbe:DB$ PumpAll | ValidCards$ Creature.YouCtrl | KW$ Vigilance | ConditionPlayerTurn$ False | ConditionPhases$ Nonsense\n" +
	"Oracle:Probe card.\n"

// condPhasesCreature puts a fixture bear on seat 0's battlefield and
// returns its id.
func condPhasesCreature(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	return putCreature(t, e, 0, condPhasesBearSrc)
}

// condPhasesDeck builds a seat-zero-start engine whose library leads with
// the named corpus cards followed by Forests/Bears; the opponent plays
// Forests only.
func condPhasesDeck(t *testing.T, reg *cards.Registry, seed uint64, fixtures ...string) (*Engine, Config) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := make([]*cards.Card, 0, 40)
	for _, name := range fixtures {
		deck = append(deck, searchCorpusCard(t, reg, name))
	}
	// Two authored fixture bears right behind the corpus fixtures: dealt into
	// the opening hand (7 cards), where putCreature finds them by name.
	deck = append(deck, card(t, condPhasesBearSrc), card(t, condPhasesBearSrc))
	for len(deck) < 40 {
		deck = append(deck, forest, bear)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// fundPool emits ManaAdd events into seat 0's pool without driving anywhere
// (addMana's toMain1 would drag the engine back to a Main1 that the
// opponent-turn scenarios have deliberately left) and re-asks priority.
func fundPool(t *testing.T, e *Engine, symbols string) {
	t.Helper()
	for _, r := range symbols {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
}

// castFormationNow waits until seat 0 holds priority, casts the named
// instant from seat 0's hand, and drains the stack.
func castFormationNow(t *testing.T, e *Engine, name string) {
	t.Helper()
	// The genesis shuffle does not preserve deck order: find the spell in
	// hand/library and put it in hand first.
	searchMoveByName(t, e, name, state.ZHand)
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Player == 0 && d.Kind == decision.KPriority {
			break
		}
		submitPass(t, e)
	}
	opt := castByName(t, e, 0, name)
	if opt == nil {
		t.Fatalf("%s not offered as a cast: %+v", name, e.Pending())
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
}

// counterN returns the number of `kind` counters on o.
func counterN(o *state.Object, kind string) int {
	for i := range o.Counters {
		if o.Counters[i].Kind == kind {
			return int(o.Counters[i].N)
		}
	}
	return 0
}

// assertAddendum asserts the addendum's two signatures on each creature:
// vigilance and one +1/+1 counter.
func assertAddendum(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil {
			t.Fatalf("creature %d missing", id)
		}
		if !e.HasKeyword(id, "Vigilance") {
			t.Fatalf("creature %d lacks the addendum's vigilance", id)
		}
		if counterN(o, "P1P1") != 1 {
			t.Fatalf("creature %d has %d P1P1 counters, want 1", id, counterN(o, "P1P1"))
		}
	}
}

// assertNoAddendum asserts the addendum was withheld on each creature while
// the UNGATED indestructible (the main line) still landed.
func assertNoAddendum(t *testing.T, e *Engine, ids ...state.ObjID) {
	t.Helper()
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil {
			t.Fatalf("creature %d missing", id)
		}
		if e.HasKeyword(id, "Vigilance") {
			t.Fatalf("creature %d gained vigilance outside the addendum's window", id)
		}
		if n := counterN(o, "P1P1"); n != 0 {
			t.Fatalf("creature %d gained %d P1P1 counters outside the addendum's window", id, n)
		}
		if !e.HasKeyword(id, "Indestructible") {
			t.Fatalf("creature %d lost the ungated indestructible", id)
		}
	}
}

// TestUnbreakableFormationAddendumOnOwnMainPhase is Leaf A(a): the real
// corpus card cast during seat 0's Main1 gets the full addendum — vigilance
// AND the +1/+1 counters (and the ungated indestructible).
func TestUnbreakableFormationAddendumOnOwnMainPhase(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := condPhasesDeck(t, reg, 9204, "Unbreakable Formation")
	c1 := condPhasesCreature(t, e)
	c2 := condPhasesCreature(t, e)
	addMana(t, e, 0, "WWWW")
	castFormationNow(t, e, "Unbreakable Formation")
	assertAddendum(t, e, c1, c2)
	replayCheck(t, e, cfg)
}

// TestUnbreakableFormationAddendumOnOpponentTurn is Leaf A(b): cast during
// the opponent's end step (ConditionPlayerTurn$ True unmet AND
// ConditionPhases$ Main1,Main2 unmet) only the ungated indestructible
// lands — no vigilance keyword, no counters.
func TestUnbreakableFormationAddendumOnOpponentTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := condPhasesDeck(t, reg, 9205, "Unbreakable Formation")
	c1 := condPhasesCreature(t, e)
	c2 := condPhasesCreature(t, e)
	// Drive to the opponent's (seat 1) end step of turn 2. The engine's
	// step boundary empties pools (CR 500.4), so fund AFTER driving.
	driveToStepAll(t, e, 2, 1, state.StepEnd)
	fundPool(t, e, "WWWW")
	castFormationNow(t, e, "Unbreakable Formation")
	assertNoAddendum(t, e, c1, c2)
	replayCheck(t, e, cfg)
}

// TestUnbreakableFormationAddendumOnOwnEndStep isolates the PHASES key: on
// the caster's OWN turn but outside Main1/Main2 (the end step),
// ConditionPlayerTurn$ True is met and the addendum is still withheld —
// the two keys gate independently, not by their conjunction alone.
func TestUnbreakableFormationAddendumOnOwnEndStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := condPhasesDeck(t, reg, 9206, "Unbreakable Formation")
	c1 := condPhasesCreature(t, e)
	c2 := condPhasesCreature(t, e)
	driveToStepAll(t, e, 1, 0, state.StepEnd)
	fundPool(t, e, "WWWW")
	castFormationNow(t, e, "Unbreakable Formation")
	assertNoAddendum(t, e, c1, c2)
	replayCheck(t, e, cfg)
}

// TestEddymurkCrabEntersTappedOnlyOnOpponentsTurn is Leaf B: the REAL
// corpus card's ETB replacement carries ConditionPlayerTurn$ False — the
// enters-tapped tap applies on an opponent's turn and NOT on the
// controller's own turn (honoured both ways, not by absence).
func TestEddymurkCrabEntersTappedOnlyOnOpponentsTurn(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	// Own turn: the ConditionPlayerTurn$ False gate is NOT met — untapped.
	e, cfg := condPhasesDeck(t, reg, 9207, "Eddymurk Crab")
	id := searchMoveByName(t, e, "Eddymurk Crab", state.ZBattlefield)
	if e.G.Obj(id).Tapped {
		t.Fatal("Eddymurk Crab entered tapped on its controller's own turn")
	}
	replayCheck(t, e, cfg)

	// Opponent's turn: the gate IS met — tapped.
	e2, cfg2 := condPhasesDeck(t, reg, 9208, "Eddymurk Crab")
	driveToStepAll(t, e2, 2, 1, state.StepMain1)
	id2 := searchMoveByName(t, e2, "Eddymurk Crab", state.ZBattlefield)
	if !e2.G.Obj(id2).Tapped {
		t.Fatal("Eddymurk Crab entered untapped on an opponent's turn")
	}
	replayCheck(t, e2, cfg2)
}

// TestConditionPhasesUnknownValueFailsOpen is Leaf C: a ConditionPhases$
// value the shared parser cannot resolve leaves the whole gate UNSUPPORTED,
// so the sub runs unconditionally even though the readable
// ConditionPlayerTurn$ False beside it says skip. Pinned so a future parser
// change cannot silently silence cards.
func TestConditionPhasesUnknownValueFailsOpen(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 9209, condPhasesFailOpenSrc, condPhasesBearSrc, condPhasesBearSrc)
	c1 := condPhasesCreature(t, e)
	c2 := condPhasesCreature(t, e)
	addMana(t, e, 0, "W")
	castFormationNow(t, e, "Phase Probe")
	for _, id := range []state.ObjID{c1, c2} {
		if !e.HasKeyword(id, "Vigilance") {
			t.Fatalf("unparseable ConditionPhases$ value must fail OPEN: creature %d's probed sub ran unconditionally and granted vigilance", id)
		}
	}
	replayCheck(t, e, cfg)
}
