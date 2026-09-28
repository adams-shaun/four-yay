package rules

// api:TakeInitiative (CR 726, "The Initiative"). These tests prove the
// primitive is registered for every corpus carrier, that Avenging Hunter's
// ETB gives its controller the initiative and ventures into Undercity (the
// Secret Entrance basic-land search resolving), that the holder's next upkeep
// ventures again, that combat damage to the holder moves the designation and
// ventures for the new holder, and that the whole scenario replays to the
// same chain head.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// initiativeExceptionTable is the corpus-walk ratchet's named exception
// table: each entry names the primitives a TakeInitiative carrier is STILL
// missing. api:TakeInitiative itself must never appear here; an entry whose
// card is now fully supported is stale and fails, and a carrier missing
// something not listed is a new gap and fails.
var initiativeExceptionTable = map[string][]string{
	"Loot Dispute":      {"trig:DungeonCompleted"},
	"Sarevok's Tome":    {"count:Initiative"},
	"Undercellar Sweep": {"count:PlayerCountDefinedTriggeredAttackedTargetAndYou$HasPropertyhasInitiative"},
}

// TestTakeInitiativePrimitiveRegisteredForEveryCorpusCarrier walks the corpus
// for every card whose parsed primitives include api:TakeInitiative and
// asserts the registry names it supported, with every remaining gap named in
// the exception table above. The carrier count is pinned so a corpus-pin
// change is noticed rather than silently skipped.
func TestTakeInitiativePrimitiveRegisteredForEveryCorpusCarrier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	if !supported["api:TakeInitiative"] {
		t.Fatal("effects.Supported() does not name api:TakeInitiative: the primitive is not registered")
	}
	carriers := 0
	for _, c := range reg.Cards {
		if !slices.Contains(c.Primitives(), "api:TakeInitiative") {
			continue
		}
		carriers++
		name := c.Faces[0].Name
		miss := reg.Unsupported(c, supported)
		if slices.Contains(miss, "api:TakeInitiative") {
			t.Errorf("card %q still reports api:TakeInitiative unsupported", name)
			continue
		}
		want, ok := initiativeExceptionTable[name]
		if len(miss) == 0 && ok {
			t.Errorf("stale exception: card %q is now fully supported; drop it from initiativeExceptionTable", name)
		}
		if len(miss) > 0 && !slices.Equal(miss, want) {
			t.Errorf("card %q missing %v; initiativeExceptionTable holds %v", name, miss, want)
		}
	}
	if carriers != 23 {
		t.Errorf("corpus carriers of api:TakeInitiative = %d, want 23 (the measured count at this corpus pin)", carriers)
	}
}

// initiativeEngine builds an N-seat game (one deck argument per seat; nil
// means mountains only) with the corpus token scripts wired in (the
// Undercity dungeon script among them), seat 0's turn parked at main1 and no
// pending decision, so a caller can place cards and drive the engine itself.
// The two-seat form is the original shape; the CR 726.4 leave-game tests pass
// three decks so a seat can depart without ending the game.
func initiativeEngine(t *testing.T, reg *cards.Registry, decks ...[]*cards.Card) (*Engine, Config) {
	t.Helper()
	names := make([]string, len(decks))
	full := make([][]*cards.Card, len(decks))
	for i, d := range decks {
		names[i] = string(rune('a' + i))
		d = append([]*cards.Card{}, d...)
		full[i] = append(d, mountainDeck(t, 40-len(d))...)
	}
	cfg := seatZeroStart(Config{Seed: 91, Names: names, Tokens: reg.Tokens,
		Decks: full})
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

// initiativeVentureQueued reports whether the source-less CR 726.2 venture
// trigger is pending for p.
func initiativeVentureQueued(e *Engine, p state.PlayerID) bool {
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].InitiativeVenture && e.pendingTriggers[i].Controller == p {
			return true
		}
	}
	return false
}

// initiativeVentureEvents counts the logged CR 726.2 venture pushes for p.
// The pending trigger becomes this event as the priority round pushes it, so
// it is the durable proof the inherent ability fired even after the queue is
// drained.
func initiativeVentureEvents(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush && ev.Player == p && ev.Counter == "__initiative_venture" {
			n++
		}
	}
	return n
}

// drainInitiative drives the engine with the deterministic first option until
// no stack object, pending trigger or decision remains. It fails loudly on an
// unanswerable decision rather than stalling the test.
func drainInitiative(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
				return
			}
			e.Advance()
			continue
		}
		if len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 && d.Kind == decision.KPriority {
			return
		}
		if len(d.Options) == 0 {
			t.Fatalf("unanswerable decision while draining: %+v", d)
		}
		pick := 0
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pick = o.Index
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
			t.Fatalf("drain submit of %v: %v", d.Kind, err)
		}
	}
	t.Fatal("initiative scenario never settled")
}

// initiativeReplayHead replays the game from its own log into a fresh state
// and asserts both the deep state and the recomputed chain head match.
func initiativeReplayHead(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	replayCheck(t, e, cfg)
	lg := events.NewLog(cfg.Seed)
	g := state.NewGameLife(cfg.Names, 20)
	g.Tokens = cfg.Tokens
	for i := range cfg.Decks {
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(cfg.Decks[i]))
		for _, c := range cfg.Decks[i] {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		g.SetZone(state.ZLibrary, p, ids)
	}
	for _, ev := range e.L.Events {
		events.Emit(g, lg, ev)
	}
	if lg.Head() != e.L.Head() {
		t.Fatalf("chain head %s, log replay %s", e.L.Head(), lg.Head())
	}
}

// resolveAvengingHunterETB moves the corpus Avenging Hunter onto seat 0's
// battlefield (a LOGGED MoveZone, so a replay reconstructs it), asserts its
// ChangesZone ETB really queued the DB$ TakeInitiative trigger, resolves it
// and drives the Secret Entrance room ability's basic-land search to rest.
// It returns the dungeon's object id, asserting the enter landed on the
// Undercity's first room.
func resolveAvengingHunterETB(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	if e.G.HasInitiative {
		t.Fatalf("precondition: game began with the initiative (who=%d)", e.G.Initiative)
	}
	id := moveByName(t, e, 0, "Avenging Hunter", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Avenging Hunter not on the battlefield: %+v", o)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Avenging Hunter ETB queued %d triggers, want 1", len(e.pendingTriggers))
	}
	drainInitiative(t, e)
	if !e.G.IsInitiative(0) {
		t.Fatalf("ETB did not give seat 0 the initiative: has=%v who=%d", e.G.HasInitiative, e.G.Initiative)
	}
	dobj, room := dungeonRoom(t, e, 0)
	if got := e.G.Obj(dobj).Face().Name; got != "Undercity" {
		t.Fatalf("entered dungeon %q, want Undercity", got)
	}
	if room != "Entrance" {
		t.Fatalf("marker on %q, want Undercity's first room Entrance", room)
	}
	if e.G.Obj(dobj).Zone != state.ZCommand {
		t.Fatalf("dungeon zone %s, want command zone", e.G.Obj(dobj).Zone)
	}
	// The Secret Entrance room ability searched a basic land into hand: the
	// one hand card over the dealt seven proves the search resolved rather
	// than the room ability being inert.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 8 {
		t.Fatalf("Secret Entrance search left %d cards in hand, want 8 (the dealt seven plus one land)", got)
	}
	return dobj
}

// TestAvengingHunterETBTakesInitiativeAndVenturesIntoUndercity is the
// brief's first Avenging Hunter leaf: the ETB gives its controller the
// initiative and ventures into Undercity, marker on Secret Entrance, and the
// basic-land search resolves.
func TestAvengingHunterETBTakesInitiativeAndVenturesIntoUndercity(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hunter := mustCorpusCard(t, reg, "Avenging Hunter")
	e, cfg := initiativeEngine(t, reg, []*cards.Card{hunter}, nil)
	resolveAvengingHunterETB(t, e)
	initiativeReplayHead(t, e, cfg)
}

// runInitiativeUpkeepScenario is the fully LOGGED scenario (the Avenging
// Hunter enters through a MoveZone, every step transition is an emitted
// event) used by both the upkeep leaf and the replay leaf, so the replay is
// exact rather than reconstructing a directly-mutated board.
func runInitiativeUpkeepScenario(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	hunter := mustCorpusCard(t, reg, "Avenging Hunter")
	e, cfg := initiativeEngine(t, reg, []*cards.Card{hunter}, nil)
	resolveAvengingHunterETB(t, e)

	// A non-holder's upkeep must queue nothing: seat 1 has no initiative.
	// beginTurn is the real turn entry (it runs the untap and upkeep
	// turn-based actions through finishEnteredStep), so this exercises the
	// same path a live turn takes.
	e.pending = nil
	e.beginTurn(1)
	if initiativeVentureQueued(e, 1) {
		t.Fatal("a non-holder's upkeep queued the initiative venture")
	}
	if e.G.Players[1].DungeonObj != 0 {
		t.Fatalf("precondition: seat 1 unexpectedly owns a dungeon %d", e.G.Players[1].DungeonObj)
	}
	if _, roomBefore := dungeonRoom(t, e, 0); roomBefore != "Entrance" {
		t.Fatalf("precondition: seat 0 marker %q, want Entrance", roomBefore)
	}

	// The holder's next upkeep queues the CR 726.2 venture.
	e.pending = nil
	e.beginTurn(0)
	if !initiativeVentureQueued(e, 0) {
		t.Fatal("the initiative holder's upkeep did not queue the venture into Undercity")
	}
	drainInitiative(t, e)
	if n := initiativeVentureEvents(e, 0); n != 1 {
		t.Fatalf("the initiative holder's upkeep pushed %d ventures, want 1", n)
	}
	_, room := dungeonRoom(t, e, 0)
	if room != "Forge" {
		t.Fatalf("upkeep venture marker %q, want Forge (Secret Entrance's first printed arrow)", room)
	}
	return e, cfg
}

// TestInitiativeHolderVenturesAtTheirUpkeep is the brief's second leaf: the
// holder's next upkeep ventures again (a non-holder's upkeep does not).
func TestInitiativeHolderVenturesAtTheirUpkeep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := runInitiativeUpkeepScenario(t, reg)
	initiativeReplayHead(t, e, cfg)
}

// combatBearSrc is the inline attacker/blocker fixture (never a corpus .txt).
const combatBearSrc = "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// runInitiativeCombatScenario gives seat 0 the initiative, has a creature
// seat 1 controls attack seat 0 unblocked, and returns the engine once the
// combat damage has landed and the resulting venture has settled.
func runInitiativeCombatScenario(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	bear := card(t, combatBearSrc)
	e, cfg := initiativeEngine(t, reg, nil, []*cards.Card{bear})
	// Seat 0 holds the initiative (the ETB path is proven separately).
	e.emit(events.Event{Kind: events.InitiativeChange, Player: 0})
	if !e.G.IsInitiative(0) {
		t.Fatalf("precondition: seat 0 does not hold the initiative (has=%v who=%d)", e.G.HasInitiative, e.G.Initiative)
	}
	atk := onBoardReady(t, e, 1, combatBearSrc)
	if o := e.G.Obj(atk); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: attacker not a ready seat-1 creature: %+v", o)
	}
	if e.Power(atk) != 2 {
		t.Fatalf("precondition: attacker power %d, want 2", e.Power(atk))
	}
	life0 := e.G.Players[0].Life

	// Seat 1 is the active attacker; seat 0 is the defending holder. Reach
	// seat 1's declare-attackers step through LOGGED events so the replay
	// rebuilds the same combat.
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.pending = nil
	e.askAttackers()
	submitAttackersOnly(t, e, atk)
	if d := e.Pending(); d != nil && d.Kind == decision.KBlockers {
		submitBlockersOnly(t, e)
	}
	drainCombatPriority(t, e)

	// The combat hit landed and moved the designation.
	if got := e.G.Players[0].Life; got != life0-2 {
		t.Fatalf("combat damage left seat 0 at %d life, want %d", got, life0-2)
	}
	if !e.G.IsInitiative(1) {
		t.Fatalf("combat damage to the holder did not move the initiative: who=%d", e.G.Initiative)
	}
	drainInitiative(t, e)
	if n := initiativeVentureEvents(e, 1); n != 1 {
		t.Fatalf("the combat-damage take pushed %d ventures, want 1", n)
	}
	dobj, room := dungeonRoom(t, e, 1)
	if got := e.G.Obj(dobj).Face().Name; got != "Undercity" {
		t.Fatalf("new holder's dungeon %q, want Undercity", got)
	}
	if room != "Entrance" {
		t.Fatalf("new holder's marker %q, want Entrance", room)
	}
	return e, cfg
}

// TestCombatDamageToInitiativeHolderMovesItAndVentures is the brief's third
// leaf: an opponent's creature dealing combat damage to the holder moves the
// initiative to that opponent and ventures for them. The combat board is
// fabricated directly (not replay-asserted here); the replay leaf below
// covers the logged path.
func TestCombatDamageToInitiativeHolderMovesItAndVentures(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	runInitiativeCombatScenario(t, reg)
}

// TestInitiativeReplayHead is the brief's standalone replay leaf: the fully
// logged ETB + upkeep game rebuilt from the log has byte-identical state and
// the same chain head.
func TestInitiativeReplayHead(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := runInitiativeUpkeepScenario(t, reg)
	initiativeReplayHead(t, e, cfg)
}

// ---- CR 726.4: the initiative holder leaving the game ----

// initiativeHandoffScenario builds a three-seat game (so one seat can depart
// without ending it) in which holder holds the initiative, granted through a
// LOGGED InitiativeChange -- the same shape the combat scenario uses -- and
// asserts the precondition the CR 726.4 handoff reads.
func initiativeHandoffScenario(t *testing.T, reg *cards.Registry, holder state.PlayerID) (*Engine, Config) {
	t.Helper()
	e, cfg := initiativeEngine(t, reg, nil, nil, nil)
	e.emit(events.Event{Kind: events.InitiativeChange, Player: holder})
	if !e.G.IsInitiative(holder) {
		t.Fatalf("precondition: seat %d does not hold the initiative (has=%v who=%d)",
			holder, e.G.HasInitiative, e.G.Initiative)
	}
	return e, cfg
}

// assertInitiativeHandoff is the shared tail of the leave-game leaves: the
// lost seat must no longer project as the holder, the new holder must hold
// it, the CR 726.2 venture must have been queued and resolved for the new
// holder (Undercity, first room), and the whole game must replay to the same
// chain head.
func assertInitiativeHandoff(t *testing.T, e *Engine, cfg Config, lost, newHolder state.PlayerID) {
	t.Helper()
	if e.G.IsInitiative(lost) {
		t.Fatalf("the departed seat %d still projects as the initiative holder (who=%d)",
			lost, e.G.Initiative)
	}
	if !e.G.IsInitiative(newHolder) {
		t.Fatalf("seat %d did not take the initiative on seat %d's departure (has=%v who=%d)",
			newHolder, lost, e.G.HasInitiative, e.G.Initiative)
	}
	if !initiativeVentureQueued(e, newHolder) && initiativeVentureEvents(e, newHolder) == 0 {
		t.Fatalf("the CR 726.4 handoff neither queued nor resolved the CR 726.2 venture for the new holder %d", newHolder)
	}
	drainInitiative(t, e)
	if n := initiativeVentureEvents(e, newHolder); n != 1 {
		t.Fatalf("the CR 726.4 handoff pushed %d ventures for the new holder %d, want 1", n, newHolder)
	}
	dobj, room := dungeonRoom(t, e, newHolder)
	if got := e.G.Obj(dobj).Face().Name; got != "Undercity" {
		t.Fatalf("new holder %d's dungeon %q, want Undercity", newHolder, got)
	}
	if room != "Entrance" {
		t.Fatalf("new holder %d's marker %q, want Entrance (the venture into a fresh Undercity)", newHolder, room)
	}
	if e.G.Obj(dobj).Zone != state.ZCommand {
		t.Fatalf("dungeon zone %s, want command zone", e.G.Obj(dobj).Zone)
	}
	initiativeReplayHead(t, e, cfg)
}

// loseSeatToLethalLife drops seat p to 0 life through one logged LifeChange
// and runs the state-based sweep, so the loss enters through the ordinary
// CR 704.5a path and the ONE loss gate (rules/cantlose.go's playerLoses).
func loseSeatToLethalLife(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	if e.G.Players[p].Lost {
		t.Fatalf("precondition: seat %d has already lost", p)
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: p, Amount: -e.G.Players[p].Life})
	e.checkStateBased()
	if !e.G.Players[p].Lost {
		t.Fatalf("precondition: seat %d did not lose at 0 life", p)
	}
}

// TestInitiativeHolderLeavesHandsOffToActivePlayer is the CR 726.4 first
// half through the state-based-action loss path: the holder (seat 1) is not
// the active player, loses at 0 life, and the ACTIVE player (seat 0) takes
// the initiative at the same time the holder leaves -- venturing into the
// Undercity per CR 726.2.
func TestInitiativeHolderLeavesHandsOffToActivePlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := initiativeHandoffScenario(t, reg, 1)
	loseSeatToLethalLife(t, e, 1)
	assertInitiativeHandoff(t, e, cfg, 1, 0)
}

// TestInitiativeActiveHolderLeavesHandsOffToNextInTurnOrder is CR 726.4's
// second half through the same loss path: the holder IS the active player
// (initiativeEngine parks the game on seat 0's main1), so the next player in
// turn order takes the initiative instead -- seat 1, by NextAlive's seat
// order.
func TestInitiativeActiveHolderLeavesHandsOffToNextInTurnOrder(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := initiativeHandoffScenario(t, reg, 0)
	if e.G.Active != 0 {
		t.Fatalf("precondition: active player %d, want 0 (the seat about to leave)", e.G.Active)
	}
	loseSeatToLethalLife(t, e, 0)
	assertInitiativeHandoff(t, e, cfg, 0, 1)
}

// TestInitiativeHolderConcedingHandsOff drives the SECOND PlayerLost emit
// site (rules/legal.go's "concede" option, which deliberately bypasses the
// playerLoses gate): the holder concedes at its own priority and the active
// player takes the initiative while the game continues with the two
// surviving seats.
func TestInitiativeHolderConcedingHandsOff(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := initiativeHandoffScenario(t, reg, 1)
	// Drive seat 1 to its own priority decision: pass on seat 0's first.
	if d := e.Pending(); d != nil {
		t.Fatalf("precondition: a decision is pending before any was advanced: %+v", d)
	}
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition: expected seat 0's priority decision, got %+v", d)
	}
	submitChoices(t, e, highestPriorityOptionWithKind(t, e, "pass").Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 1 {
		t.Fatalf("precondition: after seat 0 passed, expected seat 1's priority, got %+v", d)
	}
	submitChoices(t, e, highestPriorityOptionWithKind(t, e, "concede").Index)
	if !e.G.Players[1].Lost {
		t.Fatal("precondition: seat 1 chose concede but is not Lost")
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == 1 && ev.Text == "conceded" {
			found = true
		}
	}
	if !found {
		t.Fatal("the log carries no PlayerLost \"conceded\" event for seat 1")
	}
	assertInitiativeHandoff(t, e, cfg, 1, 0)
}

// TestInitiativeLastSeatTakesItWhenActiveHolderConcedes pins the 2-player
// edge: the active player IS the holder and concedes, so the next player in
// turn order (the only other seat) takes the initiative at the same moment
// the conceder leaves -- even though the game ends right after (the venture
// trigger is queued but never resolves).
func TestInitiativeLastSeatTakesItWhenActiveHolderConcedes(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := initiativeEngine(t, reg, nil, nil)
	e.emit(events.Event{Kind: events.InitiativeChange, Player: 0})
	if !e.G.IsInitiative(0) {
		t.Fatalf("precondition: seat 0 does not hold the initiative (has=%v who=%d)", e.G.HasInitiative, e.G.Initiative)
	}
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("precondition: expected seat 0's priority decision, got %+v", d)
	}
	submitChoices(t, e, highestPriorityOptionWithKind(t, e, "concede").Index)
	if !e.G.Over || e.G.Winner != 1 || !e.G.Players[0].Lost {
		t.Fatalf("precondition: after seat 0 concedes: over=%v winner=%d seat0lost=%v",
			e.G.Over, e.G.Winner, e.G.Players[0].Lost)
	}
	if e.G.IsInitiative(0) {
		t.Fatal("the conceding holder still projects as the initiative holder")
	}
	if !e.G.IsInitiative(1) {
		t.Fatalf("the last seat did not take the initiative (has=%v who=%d)", e.G.HasInitiative, e.G.Initiative)
	}
	if !initiativeVentureQueued(e, 1) {
		t.Fatal("the CR 726.2 venture was not queued for the last seat taking the initiative")
	}
	if n := initiativeVentureEvents(e, 1); n != 0 {
		t.Fatalf("a finished game resolved %d ventures, want 0", n)
	}
	initiativeReplayHead(t, e, cfg)
}
