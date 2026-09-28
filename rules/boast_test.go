package rules

// Task kw-boast (CR 702.142): the Boast keyword and its `Boast$ True`
// ability parameter. Forge marks a Boast ability with a parameter on the
// activated ability itself, not a `K:Boast` keyword line, so the ability
// offer loop has to read it directly. The restriction has two halves --
// the source creature must have attacked this turn, and the ability may be
// activated only once each turn -- and both are read from replay-derivable
// state: Object.AttacksThisTurn (events.Apply's DeclareAttackers case,
// reset in TurnChange's per-object loop) and the activation-event scan the
// ActivationLimit$ gate uses.
//
// Broadside Bombardiers is the measured corpus carrier the census found
// (`grep -rlE 'Boast\$ ' .cards/cardsfolder` = 19 files, all `Boast$
// True`); its script is
//
//	A:AB$ DealDamage | Cost$ Sac<1/Artifact.Other;Creature.Other/...> |
//	  ValidTgts$ Any | NumDmg$ X | Boast$ True | ...
//
// The fixtures are loaded from the real compiled corpus
// (choiceCorpusCard), never copied from Forge's GPL scripts, so the leaves
// pin the card's ACTUAL script shape.
//
// TestBroadsideBombardiersBoastGateNotOfferedBeforeAttacking: with the
// source on the battlefield but no attack declared this turn, the ability
// is withheld.
// TestBroadsideBombardiersBoastGateOfferedAfterAttacking: after the source
// is declared an attacker, the ability is offered.
// TestBroadsideBombardiersBoastOncePerTurn: the first activation consumes
// the once-per-turn allowance; a second offer is withheld the same turn.
// TestBroadsideBombardiersBoastResetsNextTurn: a new turn clears the
// attacked-this-turn fact and the once-per-turn allowance, so the ability
// is offered again only after the source attacks in the new turn.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// boastFixture builds a two-seat game whose seat-0 deck is the real corpus
// Broadside Bombardiers plus `raiders` authored sacrificial creatures and
// Mountains. The Bombardiers is moved to seat 0's battlefield; the Raiders
// stay in hand or library for the caller to place. It returns the engine, the
// config and the Bombardiers id.
func boastFixture(t *testing.T, seed uint64, raiders int) (*Engine, Config, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	bomb, ok := reg.Lookup("Broadside Bombardiers")
	if !ok {
		t.Fatal("corpus fixture: Broadside Bombardiers missing")
	}
	deck := []*cards.Card{bomb}
	for i := 0; i < raiders; i++ {
		deck = append(deck, card(t, gateRaiderSrc))
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
		},
		Tokens: map[string]*cards.Card{},
	})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Broadside Bombardiers", state.ZBattlefield)
	return e, cfg, id
}

// boastOffered reports whether the Boast ability is currently offered to
// seat 0.
func boastOffered(e *Engine, id state.ObjID) bool {
	_, ok := findAbilityOption(e, id, 0)
	return ok
}

// TestBoastGateIdentities covers the identity half of the class fix at the
// unit level, because the corpus's one granted Boast carrier (Besieged
// Viking Village's `SVar:ABBoast:AB$ PutCounter | ... | Boast$ True`,
// reached through `AddAbility$`) is a GRANTED ability: a granted activation
// mints a DelayedPush whose Counter names the SVar (rules/speed.go's
// beginGrantedActivation), never an AbilityPush with an ability index -- and
// when the granting static is cross-object (the Village grants to ALL
// creatures) it mints the GrantAbilityPush twin, whose Counter names the same
// SVar. A gate that matched only AbilityPush/Amount would not see a granted
// Boast and would re-offer it every window. The test emits all three marker
// shapes and asserts the once-per-turn half keys on the right one.
func TestBoastGateIdentities(t *testing.T) {
	t.Parallel()
	e, _, bomb := boastFixture(t, 925, 0)
	if e.G.Obj(bomb).AttacksThisTurn != 0 {
		t.Fatal("fixture unexpectedly starts with an attack this turn")
	}
	// Before any attack the gate is closed regardless of identity.
	if e.boastGateOK(bomb, 0, "") {
		t.Fatal("boastGateOK offered before the source attacked")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bomb}})
	// Attacked, no activation yet: every identity is open.
	if !e.boastGateOK(bomb, 0, "") {
		t.Fatal("printed identity withheld after attacking")
	}
	if !e.boastGateOK(bomb, -1, "BoastGrant") {
		t.Fatal("granted identity withheld after attacking")
	}
	// A printed activation (AbilityPush, ability index 0) consumes only the
	// printed identity; the granted identity is untouched.
	e.emit(events.Event{Kind: events.AbilityPush, Obj: bomb, Player: 0, Amount: 0})
	if e.boastGateOK(bomb, 0, "") {
		t.Fatal("printed identity not withheld after its own AbilityPush")
	}
	if !e.boastGateOK(bomb, -1, "BoastGrant") {
		t.Fatal("another identity's activation wrongly withheld the granted Boast")
	}
	// A granted activation (DelayedPush, Counter = SVar name) consumes only
	// the granted identity.
	e.emit(events.Event{Kind: events.DelayedPush, Obj: bomb, Player: 0, Amount: -1, Counter: "BoastGrant"})
	if e.boastGateOK(bomb, -1, "BoastGrant") {
		t.Fatal("granted identity not withheld after its own DelayedPush")
	}
	// A CROSS-object grant (a printed Continuous AddAbility$ static) mints
	// through GrantAbilityPush, whose Counter names the same SVar; it must
	// consume the granted identity exactly as DelayedPush does, or Besieged
	// Viking Village's granted Boast is re-offered all turn.
	if !e.boastGateOK(bomb, -1, "BoastCross") {
		t.Fatal("a fresh cross-object granted identity should start open")
	}
	e.emit(events.Event{Kind: events.GrantAbilityPush, Obj: bomb, Player: 0, Counter: "BoastCross", IDs: []state.ObjID{bomb}})
	if e.boastGateOK(bomb, -1, "BoastCross") {
		t.Fatal("granted identity not withheld after its own GrantAbilityPush")
	}
}

// TestBroadsideBombardiersBoastGateNotOfferedBeforeAttacking: a Boast
// ability whose source has not attacked this turn is not offered, even with
// a legal sacrifice and enough mana for target selection.
func TestBroadsideBombardiersBoastGateNotOfferedBeforeAttacking(t *testing.T) {
	t.Parallel()
	e, cfg, bomb := boastFixture(t, 921, 1)
	// A sacrificial creature and red mana: the offer is withheld by the
	// Boast gate, never by an unpayable cost or a dry pool.
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	if boastOffered(e, bomb) {
		t.Fatalf("Boast ability offered before the source attacked this turn: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestBroadsideBombardiersBoastGateOfferedAfterAttacking: once the source
// is declared an attacker this turn, the Boast gate opens.
func TestBroadsideBombardiersBoastGateOfferedAfterAttacking(t *testing.T) {
	t.Parallel()
	e, cfg, bomb := boastFixture(t, 922, 1)
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRR")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bomb}})
	addMana(t, e, 0, "")
	if !boastOffered(e, bomb) {
		t.Fatalf("Boast ability withheld despite the source attacking this turn: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// TestBroadsideBombardiersBoastOncePerTurn: the first activation consumes
// the once-per-turn allowance, so a second offer the same turn is withheld.
// A real AbilityPush is emitted by submitting the offered option (the cost
// is paid through the ordinary sac flow).
func TestBroadsideBombardiersBoastOncePerTurn(t *testing.T) {
	t.Parallel()
	e, cfg, bomb := boastFixture(t, 923, 2)
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bomb}})
	addMana(t, e, 0, "")
	if !boastOffered(e, bomb) {
		t.Fatalf("Boast ability not offered after attacking: %+v", e.Pending().Options)
	}
	opt := abilityOption(t, e, bomb, 0)
	submitChoices(t, e, opt.Index)
	// The activation pays the sacrifice and pushes the ability; whether the
	// flow suspends on a mid-payment ask or the push lands immediately, the
	// AbilityPush marker the gate reads is in the log once payment commits.
	if _, ok := boastOfferedAfterResolution(t, e, bomb); ok {
		t.Fatalf("Boast ability offered twice in the same turn: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}

// boastOfferedAfterResolution drains any pending mid-activation asks (the
// sacrifice KChoose the cost opens) until priority returns, then reports
// whether the Boast ability is offered again. It returns ok=false when the
// offer is withheld.
func boastOfferedAfterResolution(t *testing.T, e *Engine, id state.ObjID) (decision.Option, bool) {
	t.Helper()
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			return findAbilityOption(e, id, 0)
		}
		switch d.Kind {
		case decision.KChoose:
			// Answer the first option (the sacrifice pick).
			submitChoices(t, e, d.Options[0].Index)
		default:
			// A target ask (ValidTgts$ Any): take the first option.
			submitChoices(t, e, d.Options[0].Index)
		}
	}
	t.Fatalf("activation did not settle within 16 asks")
	return decision.Option{}, false
}

// TestBroadsideBombardiersBoastResetsNextTurn: advancing to the next turn
// clears both the attacked-this-turn fact and the once-per-turn allowance,
// so the Boast ability is withheld again until the source attacks in the
// new turn.
func TestBroadsideBombardiersBoastResetsNextTurn(t *testing.T) {
	t.Parallel()
	e, cfg, bomb := boastFixture(t, 924, 2)
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	moveByName(t, e, 0, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRRR")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bomb}})
	addMana(t, e, 0, "")
	if !boastOffered(e, bomb) {
		t.Fatalf("Boast ability not offered after attacking: %+v", e.Pending().Options)
	}
	opt := abilityOption(t, e, bomb, 0)
	submitChoices(t, e, opt.Index)
	boastOfferedAfterResolution(t, e, bomb)

	// A new turn is the per-turn reset boundary: emitting the TurnChange the
	// engine folds clears AttacksThisTurn on every object and rolls the
	// activation scan's window, exactly as a real turn advance does. Seat 1's
	// turn then seat 0's, so the reset is exercised through two boundaries.
	turn := e.G.Turn
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: turn + 1})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: turn + 2})
	addMana(t, e, 0, "RRRR")
	// The new turn has no attack declared: the gate is closed again.
	if boastOffered(e, bomb) {
		t.Fatalf("Boast ability offered in the next turn before the source attacked: %+v", e.Pending().Options)
	}
	// After the source attacks in the new turn it re-opens (the once-per-turn
	// allowance reset too).
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bomb}})
	addMana(t, e, 0, "")
	if !boastOffered(e, bomb) {
		t.Fatalf("Boast ability withheld in the next turn despite the source attacking: %+v", e.Pending().Options)
	}
	replayCheck(t, e, cfg)
}
