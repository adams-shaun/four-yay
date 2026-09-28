package rules

// CR 726.2 simultaneous-pass adjudication: when creatures controlled by MORE
// THAN ONE player deal combat damage to the initiative holder in the same
// combat-damage step, the whole pass is ONE adjudication. The holder is
// snapshotted when dealing begins, the distinct landed-hit controllers are
// collected across the loop, and the one adjudication after the loop gives
// the initiative to the candidate FIRST IN TURN ORDER (APNAP from the active
// player, CR 802.4's convention) -- never to whichever hit happened to be
// processed first. Exactly one InitiativeChange folds and exactly one
// source-less CR 726.2 venture trigger is queued, no matter how many hits or
// how many distinct controllers the pass carried.
//
// The boards below are fabricated white-box fixtures (same package): they
// build e.combatRound.assignments directly, because the live flow cannot put
// two different controllers' creatures into one simultaneous pass without a
// mid-combat control change (only the active player declares attackers). The
// hit sources are still placed through LOGGED MoveZone events (moveByName),
// so the log-only replay rebuilds the same board and the replay-head leaf
// bites.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// initiativeEngine3 is initiativeEngine's three-seat sibling: the same
// seatZeroStart + logged TurnChange/StepChange shape, with a third deck
// column, because adjudicating simultaneous hits from different controllers
// needs at least three seats (a holder plus two candidate controllers).
// extras[i] seeds seat i's deck with the named card(s) the fixture will move
// onto the battlefield; the rest of the column is Mountains.
func initiativeEngine3(t *testing.T, reg *cards.Registry, extras [3][]*cards.Card) (*Engine, Config) {
	t.Helper()
	var decks [3][]*cards.Card
	for i := range decks {
		decks[i] = append(decks[i], extras[i]...)
		decks[i] = append(decks[i], mountainDeck(t, 40-len(decks[i]))...)
	}
	cfg := seatZeroStart(Config{Seed: 91, Names: []string{"a", "b", "c"},
		Tokens: reg.Tokens, Decks: decks[:]})
	e := New(cfg)
	if e.choosing == chooseOpening {
		// The fabricated board below replaces the pregame, abandoning the
		// opening-hand round New posed; its flow marker goes with it, as it
		// would once the round finished.
		e.choosing = chooseNone
	}
	// Reach seat 0's first main phase through LOGGED events (never a direct
	// Active/Step write: that state would not be in the log, and a replayed
	// game would sit on a different step).
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	return e, cfg
}

// initiativeChangesInLog lists the players every logged InitiativeChange
// named, in order.
func initiativeChangesInLog(e *Engine) []state.PlayerID {
	var out []state.PlayerID
	for _, ev := range e.L.Events {
		if ev.Kind == events.InitiativeChange {
			out = append(out, ev.Player)
		}
	}
	return out
}

type initiativeHit struct {
	ctrl   state.PlayerID
	amount int32
}

// runInitiativeSimultaneousFixture grants seat 0 the initiative through a
// logged InitiativeChange, turns the game over to `active` through logged
// events, places one ready 2/2 bear under each hit's controller through a
// logged MoveZone, and deals the given simultaneous pass with the real damage
// pipeline. Slice order is the PROCESSING order, deliberately NOT turn order
// in the callers. It asserts its own preconditions and returns the engine
// after the pass completed (assignments drained, life actually dropped).
func runInitiativeSimultaneousFixture(t *testing.T, reg *cards.Registry,
	active state.PlayerID, hits []initiativeHit) (*Engine, Config) {
	t.Helper()
	// One bear per hit, seeded into its controller's deck; the same inline
	// fixture card, so two hits under one controller move two copies.
	var extras [3][]*cards.Card
	for _, h := range hits {
		extras[h.ctrl] = append(extras[h.ctrl], card(t, combatBearSrc))
	}
	e, cfg := initiativeEngine3(t, reg, extras)
	e.emit(events.Event{Kind: events.InitiativeChange, Player: 0})
	if !e.G.IsInitiative(0) {
		t.Fatalf("precondition: seat 0 does not hold the initiative (has=%v who=%d)", e.G.HasInitiative, e.G.Initiative)
	}
	// The turn belongs to `active` (logged, so replay rebuilds it): the
	// holder seat 0 is NOT the active player, and the adjudication walks
	// AliveFrom(active), whose head is the active seat itself.
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: active, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepCombatDamage})
	e.pending = nil
	if e.G.Active != active {
		t.Fatalf("precondition: active player %d, want %d", e.G.Active, active)
	}
	if e.G.AliveFrom(active)[0] != active {
		t.Fatalf("precondition: turn order from active %d starts at %d", active, e.G.AliveFrom(active)[0])
	}

	life0 := e.G.Players[0].Life
	total := int32(0)
	as := make([]assignment, 0, len(hits))
	for _, h := range hits {
		id := moveByName(t, e, h.ctrl, "Runeclaw Bear", state.ZBattlefield)
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != h.ctrl {
			t.Fatalf("precondition: hit source not a ready seat-%d battlefield creature: %+v", h.ctrl, o)
		}
		if e.Power(id) != 2 {
			t.Fatalf("precondition: seat-%d creature power %d, want 2", h.ctrl, e.Power(id))
		}
		as = append(as, assignment{toPlayer: 0, amount: h.amount, from: id, lifelink: h.ctrl})
		total += h.amount
	}
	if total == 0 {
		t.Fatalf("precondition: a zero-damage pass cannot exercise the rule")
	}
	e.combatRound.assignments = as
	e.combatRound.damageNext = 0
	e.combatRound.dealing = true
	e.openDamageBatch()
	e.BeginLifeLossBatch()
	e.runCombatAssignments()

	if got := e.G.Players[0].Life; got != life0-total {
		t.Fatalf("precondition not discharged: holder at %d life, want %d (%d damage landed)", got, life0-total, total)
	}
	if e.combatRound.assignments != nil {
		t.Fatalf("the pass did not complete: %d assignments left parked", len(e.combatRound.assignments))
	}
	return e, cfg
}

// TestInitiativeSimultaneousHitsAdjudicateByTurnOrder is the core defect:
// two DIFFERENT controllers land combat damage on the holder in one pass,
// and seat 2's hit is PROCESSED FIRST while seat 1 is FIRST IN TURN ORDER
// (active=1: AliveFrom(1) walks 1, 2, 0). The initiative must go to seat 1
// -- the pass is adjudicated by turn order, not by event order -- with
// exactly one InitiativeChange and one venture for the winner, none for the
// loser, and a byte-identical replay.
func TestInitiativeSimultaneousHitsAdjudicateByTurnOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := runInitiativeSimultaneousFixture(t, reg, 1, []initiativeHit{
		{ctrl: 2, amount: 1}, // processed FIRST in list order...
		{ctrl: 1, amount: 1}, // ...but seat 1 is first in turn order from active=1
	})

	// Precondition of the assertion itself: the two candidates really do sit
	// in the asserted order (processed-first ≠ turn-order-first).
	if order := e.G.AliveFrom(1); len(order) < 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("precondition: turn order from seat 1 is %v, want [1 2 ...]", order)
	}

	changes := initiativeChangesInLog(e)
	if len(changes) != 2 { // the setup's grant + exactly ONE adjudication
		t.Fatalf("logged InitiativeChange events %v, want [0] plus exactly one take", changes)
	}
	if changes[1] != 1 {
		t.Fatalf("simultaneous pass gave the initiative to seat %d, want seat 1 (first in turn order, not first processed)", changes[1])
	}
	if !e.G.IsInitiative(1) {
		t.Fatalf("live designation not seat 1: who=%d", e.G.Initiative)
	}
	if !initiativeVentureQueued(e, 1) {
		t.Fatalf("the winner's CR 726.2 venture trigger was not queued")
	}
	if initiativeVentureQueued(e, 2) {
		t.Fatalf("the loser was wrongly queued a venture trigger")
	}
	drainInitiative(t, e)
	if n := initiativeVentureEvents(e, 1); n != 1 {
		t.Fatalf("winner logged %d venture pushes, want 1", n)
	}
	if n := initiativeVentureEvents(e, 2); n != 0 {
		t.Fatalf("loser logged %d venture pushes, want 0", n)
	}
	if _, room := dungeonRoom(t, e, 1); room != "Entrance" {
		t.Fatalf("winner's dungeon marker %q, want Entrance (ventured once into Undercity)", room)
	}
	initiativeReplayHead(t, e, cfg)
}

// TestInitiativeSimultaneousSameControllerOneTake is the dedupe leaf: three
// landed hits in one pass, two from seat 1's creatures and one from seat 2's,
// with the turn on seat 2 so seat 2 is FIRST in turn order even though seat
// 1's hits are processed first. Still exactly ONE InitiativeChange (to seat
// 2) and ONE venture: the candidates are a SET of distinct controllers and
// the pass is adjudicated once, not once per hit.
func TestInitiativeSimultaneousSameControllerOneTake(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := runInitiativeSimultaneousFixture(t, reg, 2, []initiativeHit{
		{ctrl: 1, amount: 1}, // processed first, but LAST in turn order
		{ctrl: 1, amount: 1}, // same controller again: deduped, no second take
		{ctrl: 2, amount: 1}, // processed last, FIRST in turn order from active=2
	})

	// Precondition of the assertion itself: from active=2 the turn order
	// walks 2 first and 1 later, the opposite of the processing order above.
	if order := e.G.AliveFrom(2); len(order) < 3 || order[0] != 2 || order[1] != 0 || order[2] != 1 {
		t.Fatalf("precondition: turn order from seat 2 is %v, want [2 0 1]", order)
	}

	changes := initiativeChangesInLog(e)
	if len(changes) != 2 { // the setup's grant + exactly ONE adjudication
		t.Fatalf("logged InitiativeChange events %v, want [0] plus exactly one take", changes)
	}
	if changes[1] != 2 {
		t.Fatalf("simultaneous pass gave the initiative to seat %d, want seat 2 (first in turn order from active=2)", changes[1])
	}
	if !e.G.IsInitiative(2) {
		t.Fatalf("live designation not seat 2: who=%d", e.G.Initiative)
	}
	if !initiativeVentureQueued(e, 2) {
		t.Fatalf("the winner's CR 726.2 venture trigger was not queued")
	}
	if initiativeVentureQueued(e, 1) {
		t.Fatalf("the loser was wrongly queued a venture trigger")
	}
	drainInitiative(t, e)
	if n := initiativeVentureEvents(e, 2); n != 1 {
		t.Fatalf("winner logged %d venture pushes, want 1", n)
	}
	if n := initiativeVentureEvents(e, 1); n != 0 {
		t.Fatalf("loser logged %d venture pushes, want 0", n)
	}
	initiativeReplayHead(t, e, cfg)
}
