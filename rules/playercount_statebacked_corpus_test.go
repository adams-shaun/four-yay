package rules

// PlayerCount state-backed HasProperty carriers, end to end on the REAL
// corpus faces: Archivist of Gondor's no-monarch gate (the monarchy-vacant
// CheckSVar$ read), Wolfcaller's Howl's per-opponent hand count driving its
// token amount, War Elemental's landed-damage ETB sacrifice gate, and Tymna
// the Weaver's no-source combat-damage ledger count. Every fixture asserts
// the exact SVar/trigger bodies it exercises before using them, and
// replay-verifies by folding the whole event log through events.Apply
// (replayFromLog) and comparing the state the property read depends on.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// pcsCorpusEngine builds a two-seat game, seat 0 active (the CR 103.1 toss
// pinned via seatZeroStart), with each named REAL corpus card parked on seat
// 0's battlefield and no pending decision. cmdName, when not empty, is ALSO
// appended to seat 0's deck and configured as seat 0's commander (it moves to
// the command zone at genesis, so it is never in a zone crAbortMove scans).
func pcsCorpusEngine(t *testing.T, cmdName string, names ...string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := mountainDeck(t, 40)
	for _, name := range names {
		deck = append(deck, mustCorpusCard(t, reg, name))
	}
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 41)}, Tokens: reg.Tokens})
	if cmdName != "" {
		deck = append(deck, mustCorpusCard(t, reg, cmdName))
		cfg.Decks[0] = deck
		cfg.Commanders = [][]int{{len(deck) - 1}, nil}
	}
	e := New(cfg)
	e.Advance()
	ids := map[string]state.ObjID{}
	for _, name := range names {
		ids[name] = crAbortMove(t, e, 0, name, state.ZBattlefield)
	}
	e.pending = nil
	return e, cfg, ids
}

// pcsMoveCards emits the real MoveZone events moving the first n cards of
// seat p's `from` zone into `to`.
func pcsMoveCards(t *testing.T, e *Engine, p state.PlayerID, n int, from, to state.Zone) {
	t.Helper()
	ids := e.G.Zone(from, p)
	if len(ids) < n {
		t.Fatalf("seat %d %s holds %d cards, want at least %d", p, from, len(ids), n)
	}
	for i := 0; i < n; i++ {
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids[i], From: from, To: to})
	}
}

// pcsCountTokens counts TokenCreate events for seat p in the log.
func pcsCountTokens(e *Engine, p state.PlayerID) int {
	return countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.TokenCreate && ev.Player == p
	})
}

// pcsStep emits the StepChange WITHOUT draining the queue (pcdrStep asserts
// and clears; these fixtures need the queued trigger to push and resolve).
func pcsStep(e *Engine, s state.Step) {
	e.emit(events.Event{Kind: events.StepChange, Step: s})
}

// pcsAssertSVar pins a corpus face's exact SVar body — the fixture asserts
// the string it is about to exercise before exercising it, so a corpus
// change cannot silently retarget the pin.
func pcsAssertSVar(t *testing.T, e *Engine, id state.ObjID, name, want string) {
	t.Helper()
	got, ok := e.G.Obj(id).Face().SVars[name]
	if !ok || got != want {
		t.Fatalf("corpus fixture changed: %s SVar %s = %q (present %v), want %q",
			e.G.Obj(id).Face().Name, name, got, ok, want)
	}
}

// TestArchivistOfGondorNoMonarchGate pins the card's real intervening-if:
// its commander-damage trigger fires ONLY while the monarchy is vacant —
// the CheckSVar$ Monarch gate reads
// PlayerCountPlayers$HasPropertyisMonarch EQ0, which must count the REAL
// monarch state (event-folded), and never a fabricated zero.
func TestArchivistOfGondorNoMonarchGate(t *testing.T) {
	t.Parallel()
	e, cfg, ids := pcsCorpusEngine(t, "Tymna the Weaver", "Archivist of Gondor", "Grizzly Bears")
	archivist, bears := ids["Archivist of Gondor"], ids["Grizzly Bears"]
	cmd := e.G.Players[0].Commanders[0]
	pcsAssertSVar(t, e, archivist, "Monarch", "PlayerCountPlayers$HasPropertyisMonarch")
	tr := crTriggerFixture(t, e, archivist, "DamageDone", "BecomeMonarch")
	if tr.Params["CheckSVar"] != "Monarch" || tr.Params["SVarCompare"] != "EQ0" {
		t.Fatalf("corpus fixture changed: CheckSVar=%q SVarCompare=%q, want Monarch/EQ0",
			tr.Params["CheckSVar"], tr.Params["SVarCompare"])
	}
	if e.G.HasMonarch {
		t.Fatalf("precondition: game began with a monarch (%d)", e.G.Monarch)
	}

	// Non-commander combat damage: the source spec fails, nothing fires.
	pcdrCombat(e, bears, 1, 3)
	if n := queuedPhaseTriggers(e, archivist); n != 0 {
		e.pendingTriggers = nil
		t.Fatalf("non-commander combat damage queued %d triggers, want 0", n)
	}

	// The commander deals combat damage with the monarchy vacant: the gate
	// holds and the trigger fires.
	pcdrCombat(e, cmd, 1, 3)
	if n := queuedPhaseTriggers(e, archivist); n != 1 {
		e.pendingTriggers = nil
		t.Fatalf("commander combat damage with no monarch queued %d triggers, want 1", n)
	}
	e.pendingTriggers = nil

	// An opponent takes the crown: the same commander damage now reads a
	// nonzero count and the EQ0 gate withholds the trigger.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	pcdrCombat(e, cmd, 1, 3)
	if n := queuedPhaseTriggers(e, archivist); n != 0 {
		t.Fatalf("commander combat damage with a monarch queued %d triggers, want 0", n)
	}
	e.pendingTriggers = nil

	// That same combat damage ALSO stole the crown: CR 724.2b hands the
	// monarchy to the controller of a source that deals combat damage to
	// the monarch, so seat 0's commander took it back from seat 1. The gate
	// above read the pre-damage state (seat 1 monarch, count 1, EQ0 fails);
	// the state the log now folds to is seat 0.
	if !e.G.IsMonarch(0) {
		t.Fatalf("after combat damage to the monarch: monarch = seat %d (has %v), want seat 0",
			e.G.Monarch, e.G.HasMonarch)
	}

	// Replay: folding the whole log back through events.Apply reproduces
	// the monarch state the gate read (and the combat hit it rode on).
	// The full-game diff is not available here: replayFromLog folds events
	// without re-running New's commander bookkeeping (Player.Commanders is
	// a New-side list, not an event field), so the command-zone membership
	// and the monarch state are asserted directly.
	re := replayFromLog(t, cfg, e.L.Events)
	if !containsID(re.Zone(state.ZCommand, 0), cmd) {
		t.Fatal("replayed command zone does not hold the commander")
	}
	if !re.IsMonarch(0) {
		t.Fatalf("replayed monarch state = seat %d (has %v), want seat 0", re.Monarch, re.HasMonarch)
	}
}

// TestWolfcallersHowlCountsOpponentsHands pins the per-opponent zone count:
// the upkeep trigger's TokenAmount$ X reads
// PlayerCountOpponents$HasPropertyHasCardsInHand_Card_GE4, so X — and only
// X — tracks how many opponents hold four or more cards.
func TestWolfcallersHowlCountsOpponentsHands(t *testing.T) {
	t.Parallel()
	e, cfg, ids := pcsCorpusEngine(t, "", "Wolfcaller's Howl")
	howl := ids["Wolfcaller's Howl"]
	pcsAssertSVar(t, e, howl, "X", "PlayerCountOpponents$HasPropertyHasCardsInHand_Card_GE4")

	// Three cards in the opponent's hand: below the boundary, X = 0, no
	// token. The opening hand dealt 7; four go back to the library first.
	if got := len(e.G.Zone(state.ZHand, 1)); got != 7 {
		t.Fatalf("precondition: opponent opening hand holds %d cards, want 7", got)
	}
	pcsMoveCards(t, e, 1, 4, state.ZHand, state.ZLibrary)
	if got := len(e.G.Zone(state.ZHand, 1)); got != 3 {
		t.Fatalf("precondition: opponent hand holds %d cards, want 3", got)
	}
	before := pcsCountTokens(e, 0)
	pcsStep(e, state.StepUpkeep)
	if n := queuedPhaseTriggers(e, howl); n != 1 {
		e.pendingTriggers = nil
		t.Fatalf("upkeep with a 3-card opponent hand queued %d triggers, want 1 (the trigger fires; only X is gated)", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	e.pending = nil
	if got := pcsCountTokens(e, 0) - before; got != 0 {
		t.Fatalf("3-card hand minted %d tokens, want 0 (X = 0)", got)
	}

	// The fourth card crosses the boundary: X = 1, one Wolf.
	pcsMoveCards(t, e, 1, 1, state.ZLibrary, state.ZHand)
	before = pcsCountTokens(e, 0)
	pcsStep(e, state.StepUpkeep)
	if n := queuedPhaseTriggers(e, howl); n != 1 {
		e.pendingTriggers = nil
		t.Fatalf("upkeep with a 4-card opponent hand queued %d triggers, want 1", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	e.pending = nil
	if got := pcsCountTokens(e, 0) - before; got != 1 {
		t.Fatalf("4-card hand minted %d tokens, want 1 (X = 1)", got)
	}

	// Wrong zone: the qualifying cards move back to the library — the hand
	// count drops below the boundary again and no further token is minted.
	pcsMoveCards(t, e, 1, 4, state.ZHand, state.ZLibrary)
	before = pcsCountTokens(e, 0)
	pcsStep(e, state.StepUpkeep)
	e.putTriggersOnStack()
	e.resolveTop()
	e.pending = nil
	if got := pcsCountTokens(e, 0) - before; got != 0 {
		t.Fatalf("cards back in the library minted %d tokens, want 0", got)
	}

	// Replay: the folded log reproduces the game the head priced, tokens
	// and all.
	re := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, re); diff != "" {
		t.Fatalf("replayed game diverged: %s", diff)
	}
}

// TestWarElementalSacrificeGateOnLandedDamage pins the ETB sacrifice gate:
// War Elemental sacrifices itself unless an opponent was dealt damage this
// turn, the ConditionCheckSVar$ EQ0 read of
// PlayerCountRegisteredOpponents$HasPropertywasDealtDamageThisTurn — real
// player hits satisfy it, and damage a redirect moved onto a PERMANENT does
// not.
func TestWarElementalSacrificeGateOnLandedDamage(t *testing.T) {
	t.Parallel()
	// No damage anywhere: the gate holds (count 0, EQ0) and War Elemental
	// sacrifices itself on entry.
	e, _, ids := pcsCorpusEngine(t, "", "War Elemental", "Grizzly Bears")
	we := ids["War Elemental"]
	pcsAssertSVar(t, e, we, "WarElementalX", "PlayerCountRegisteredOpponents$HasPropertywasDealtDamageThisTurn")
	if n := e.DamageTakenThisTurn(1); n != 0 {
		t.Fatalf("precondition: opponent damage taken = %d, want 0", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	e.pending = nil
	if containsID(e.G.Zone(state.ZBattlefield, 0), we) {
		t.Fatal("War Elemental survived entry with no opponent damage taken, want sacrificed")
	}
	if !containsID(e.G.Zone(state.ZGraveyard, 0), we) {
		t.Fatal("sacrificed War Elemental is not in its owner's graveyard")
	}

	// A real (non-combat) hit on the opponent lands: the count is nonzero
	// and the same entry survives.
	e2, cfg2, ids2 := pcsCorpusEngine(t, "", "War Elemental", "Grizzly Bears")
	e2.damaging = ids2["Grizzly Bears"]
	e2.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	e2.damaging = 0
	if n := e2.DamageTakenThisTurn(1); n != 3 {
		t.Fatalf("precondition: opponent damage taken = %d, want 3", n)
	}
	e2.putTriggersOnStack()
	e2.resolveTop()
	e2.pending = nil
	if !containsID(e2.G.Zone(state.ZBattlefield, 0), ids2["War Elemental"]) {
		t.Fatal("War Elemental sacrificed itself despite an opponent being dealt damage this turn")
	}

	// The hit is REDIRECTED onto a permanent (the Damage event carries the
	// object, not the player): the fold must not count it, so the gate is
	// 0 again and the entry sacrifices.
	e3, _, ids3 := pcsCorpusEngine(t, "", "War Elemental", "Grizzly Bears")
	bears3 := ids3["Grizzly Bears"]
	e3.damaging = bears3
	e3.emit(events.Event{Kind: events.Damage, Obj: bears3, Amount: 3})
	e3.damaging = 0
	if n := e3.DamageTakenThisTurn(1); n != 0 {
		t.Fatalf("damage redirected onto a permanent counted for the player: %d, want 0", n)
	}
	e3.putTriggersOnStack()
	e3.resolveTop()
	e3.pending = nil
	if containsID(e3.G.Zone(state.ZBattlefield, 0), ids3["War Elemental"]) {
		t.Fatal("War Elemental survived entry when the only damage was redirected onto a permanent")
	}

	// Replay: the folded log reproduces the survival verdict of the
	// surviving scenario.
	re := replayFromLog(t, cfg2, e2.L.Events)
	if containsID(re.Zone(state.ZGraveyard, 0), ids2["War Elemental"]) {
		t.Fatal("replayed log sacrificed War Elemental, want it on the battlefield")
	}
}

// TestTymnaNoSourceCombatLedgerCountsHitOpponents pins the no-source combat
// ledger: Tymna's
// PlayerCountRegisteredOpponents$HasPropertywasDealtCombatDamageThisTurn
// counts opponents the per-turn combat ledger recorded a hit for, evaluated
// through the card's own compiled SVar on the real engine.
func TestTymnaNoSourceCombatLedgerCountsHitOpponents(t *testing.T) {
	t.Parallel()
	e, cfg, ids := pcsCorpusEngine(t, "", "Tymna the Weaver", "Grizzly Bears")
	tymna := ids["Tymna the Weaver"]
	pcsAssertSVar(t, e, tymna, "X", "PlayerCountRegisteredOpponents$HasPropertywasDealtCombatDamageThisTurn")
	body := "PlayerCountRegisteredOpponents$HasPropertywasDealtCombatDamageThisTurn"

	ctx := &effects.Ctx{Controller: 0, Source: tymna}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("empty ledger body = (%d, %v), want evaluated 0", n, ok)
	}

	// Real combat damage to the opponent through the runCombatAssignments
	// path: the ledger records the hit and the body reads 1.
	pcdrCombat(e, ids["Grizzly Bears"], 1, 3)
	hits := e.CombatDamageToPlayersThisTurn()
	if len(hits) != 1 || hits[0].Player != 1 {
		t.Fatalf("combat ledger = %+v, want one hit on seat 1", hits)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("post-combat body = (%d, %v), want 1", n, ok)
	}

	// The real postcombat-main trigger resolves to the may-pay draw ask
	// priced at that count.
	pcsStep(e, state.StepMain2)
	if n := queuedPhaseTriggers(e, tymna); n != 1 {
		t.Fatalf("postcombat main queued %d triggers, want 1", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if d := e.Pending(); d == nil {
		t.Fatal("Tymna's may-pay draw ask never surfaced after the combat hit")
	} else if d.Player != 0 {
		t.Fatalf("may-pay ask posed to seat %d, want the controller (0)", d.Player)
	}

	// Replay: the folded log reproduces the ledger the body read.
	re := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, re); diff != "" {
		t.Fatalf("replayed game diverged: %s", diff)
	}
	if got, want := re.Players[1].Life, e.G.Players[1].Life; got != want {
		t.Fatalf("replayed seat 1 life = %d, want %d", got, want)
	}
}
