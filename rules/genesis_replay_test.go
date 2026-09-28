package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// smallDeckGame builds an n-player engine with decks just barely bigger than
// the opening hand, so the players deck out (and the game reaches Over)
// after a handful of turns instead of the ~30+ turns a 40-card deck needs.
// Returns the Config used, since a fresh replay needs it (Ruling D).
func smallDeckGame(t *testing.T, n, deckSize int) (*Engine, Config) {
	t.Helper()
	names := make([]string, n)
	decks := make([][]*cards.Card, n)
	for i := range names {
		names[i] = string(rune('a' + i))
		decks[i] = mountainDeck(t, deckSize)
	}
	cfg := Config{Seed: 7, Names: names, Decks: decks}
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// driveToOver repeatedly answers "pass" until the game ends or limit
// iterations elapse. Fails the test if the game never ends.
func driveToOver(t *testing.T, e *Engine, limit int) {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		if passAll(t, e, 1) == 0 {
			break
		}
	}
	if !e.G.Over {
		t.Fatalf("game did not reach Over within %d pass rounds", limit)
	}
}

// TestGenesisStopsAtEliminationDuringTheOpeningDraw is the Ruling T22-c
// regression test: a deck smaller than the opening hand means a player
// decks out (CR 704.5c) before genesis even finishes dealing everyone their
// opening hand. Before Task 22, losing a game had no real consequence
// anywhere the genesis loop could observe -- checkGameOver alone could never
// fire mid-deal, since nothing reduced AliveCount to 1 before every seat at
// least had a full library to draw from. Task 22 makes losing real
// (checkStateBased, called from drawCard on every single draw, including
// these), so New's own per-seat loop must notice Over and stop once dealing
// further makes no sense -- not shuffle and deal a later seat's hand
// regardless, and not call beginTurn on a game that is already finished.
func TestGenesisStopsAtEliminationDuringTheOpeningDraw(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 5, Names: names, Mulligans: 3,
		Decks: [][]*cards.Card{mountainDeck(t, 3), mountainDeck(t, 40)}}
	e := New(cfg)

	if !e.G.Over {
		t.Fatal("seat 0's 3-card deck cannot fill a 7-card opening hand; the game should already be over")
	}
	if e.pregame {
		t.Fatal("the mulligan round must never start on a game already Over (decked out at genesis)")
	}
	if !e.G.Players[0].Lost {
		t.Fatal("seat 0 should be the one who decked out")
	}
	if e.G.Turn != 0 {
		t.Fatalf("Turn = %d, want 0: beginTurn must not run once genesis has already ended the game", e.G.Turn)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != 0 {
		t.Fatalf("seat 1 hand = %d cards, want 0: genesis must stop before dealing a later "+
			"seat once an earlier one has already ended the game", got)
	}
	if e.Pending() != nil {
		t.Fatalf("a decision is pending on an already-finished game: %+v", e.Pending())
	}
}

// TestGenesisBeginsWithTheFirstAliveSeat is the Ruling T22-f regression test
// (fix round 1): the previous fix only ever checked Over, which is false
// whenever the seat that decked out is not the LAST one -- three seats,
// only the first eliminated, still leaves two others to play the game out.
// Genesis used to end with an unconditional beginTurn(0) regardless, handing
// turn 1 to a player who is already out of the game. It must go to a seat
// still alive instead -- under the CR 103.1 toss EITHER survivor is a valid
// starting seat (the toss is uniform over the survivors; the census pinning
// that on this exact config is TestTossIsUniformOverGenesisSurvivors), so
// the assertion is "the start is a survivor and turn 1 runs for it", never
// "the start is seat 1".
func TestGenesisBeginsWithTheFirstAliveSeat(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b", "c"}
	cfg := Config{Seed: 5, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 3), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	e.Advance()

	if e.G.Over {
		t.Fatal("two seats remain after seat 0 decks out; the game is not over")
	}
	if !e.G.Players[0].Lost {
		t.Fatal("seat 0 should be the one who decked out")
	}
	alive := map[state.PlayerID]bool{1: true, 2: true}
	if !alive[e.G.Active] {
		t.Fatalf("Active = %d, an eliminated seat must not receive turn 1; want a survivor (1 or 2)", e.G.Active)
	}
	if e.G.Turn != 1 {
		t.Fatalf("Turn = %d, want 1: the game should still have started for the surviving seats", e.G.Turn)
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("expected a live decision -- the game did start, just for a survivor")
	}
	if d.Player != e.G.Active {
		t.Fatalf("decision handed to seat %d, want the starting seat %d: %+v", d.Player, e.G.Active, d)
	}
}

// TestNewWithNoSeatsDoesNotPanic is the Ruling T22-e regression test: a
// zero-seat Config used to panic inside beginTurn(0) -- Zone(ZBattlefield, 0)
// indexes a zones slice sized numZones*len(Players), which is empty with no
// players at all. Pre-existing at BASE (dec046a), not introduced by Task 22,
// but the genesis rewrite that fixes T22-f touches this exact line, so a
// guarded "first alive seat" should make the zero-seat case fall out safely
// rather than reaching index-out-of-range. A game with nobody in it is
// trivially over (CR 104.4a's draw, vacuously) rather than a live game
// nobody can ever submit anything to.
func TestNewWithNoSeatsDoesNotPanic(t *testing.T) {
	t.Parallel()
	e := New(Config{})
	if !e.G.Over {
		t.Fatal("a zero-seat game should already be over")
	}
	if !e.G.Draw {
		t.Fatal("a zero-seat game has no winner to name -- it should be recorded as a draw")
	}
	if e.Pending() != nil {
		t.Fatalf("a decision is pending on a zero-seat game: %+v", e.Pending())
	}
}

// TestNewWithMoreDecksThanNamesDoesNotPanic is the Ruling T22-m regression
// test (fix round 2): a Config carrying more decks than named seats used to
// panic inside the per-deck loop, before New's zero-alive guard (this same
// function, added for T22-e) is ever reached -- state.NewGame sizes
// g.zones from len(cfg.Names) alone, so the deck loop's own
// SetZone(ZLibrary, PlayerID(i), ...) call for i >= len(cfg.Names) indexes
// outside it. Task 25 wires Config from a client, so a malformed one (the
// same motivation as T22-e last round) must degrade rather than crash the
// one goroutine running the whole match. A deck with no seat to belong to
// is simply never dealt; with zero real seats left, the game is trivially
// over, same as the zero-seat case just above.
func TestNewWithMoreDecksThanNamesDoesNotPanic(t *testing.T) {
	t.Parallel()
	e := New(Config{Decks: [][]*cards.Card{mountainDeck(t, 40)}})
	if !e.G.Over {
		t.Fatal("a Config with no named seats at all should already be over")
	}
	if !e.G.Draw {
		t.Fatal("a Config with no named seats has no winner to name -- it should be recorded as a draw")
	}
	if e.Pending() != nil {
		t.Fatalf("a decision is pending: %+v", e.Pending())
	}
}

// TestReplayReconstructsPassesAndPriority is the Ruling A regression test:
// Passes and Priority must flow through events, not direct field writes, so
// that replaying the log alone (no rules-package code involved) reproduces
// them. If a write to G.Passes or G.Priority ever bypasses an emit again,
// the live value will drift from what the log can reconstruct and this test
// fails.
func TestReplayReconstructsPassesAndPriority(t *testing.T) {
	t.Parallel()
	e, cfg := smallDeckGame(t, 2, 8)
	driveToOver(t, e, 5000)

	// Replay through events.Apply alone -- no Engine, no rules code. Genesis
	// (state.NewGame plus the deck-building AddObject calls in Engine.New)
	// is not itself logged (Ruling D), so it is not replayed here either;
	// Passes and Priority never depend on the object arena being populated.
	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}

	if fresh.Passes != e.G.Passes {
		t.Errorf("replayed Passes = %d, want %d (live)", fresh.Passes, e.G.Passes)
	}
	if fresh.Priority != e.G.Priority {
		t.Errorf("replayed Priority = %d, want %d (live)", fresh.Priority, e.G.Priority)
	}
}

// TestFinishedGameRejectsSubmit is the first Ruling B regression test: once
// a game ends, no further decision should be outstanding at all.
func TestFinishedGameRejectsSubmit(t *testing.T) {
	t.Parallel()
	e, _ := smallDeckGame(t, 2, 8)
	driveToOver(t, e, 5000)

	if d := e.Pending(); d != nil {
		t.Fatalf("Pending() returned a live decision after Over: %+v", d)
	}
	if err := e.Submit(decision.Intent{Player: 0, Choices: []int{0}}); err == nil {
		t.Fatalf("Submit succeeded on a finished game")
	}
}

// TestSubmitGuardsOverEvenWithStalePending is the second Ruling B regression
// test. It reproduces the reviewer's exact failure mode directly: a live
// decision left queued (however that happens) after e.G.Over is set. Submit
// must refuse it regardless of what Pending() currently holds; this is a
// second, independent gate from the priorityRound fix above.
func TestSubmitGuardsOverEvenWithStalePending(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.emit(events.Event{Kind: events.GameOver, Player: 0})
	stale := &decision.Decision{
		Player: 1, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass priority"}},
	}
	e.pending = stale

	err := e.Submit(decision.Intent{Seq: stale.Seq, Player: 1, Choices: []int{0}})
	if err == nil {
		t.Fatalf("Submit succeeded on a finished game with a stale pending decision")
	}
	if e.pending != stale {
		t.Fatalf("Submit consumed the stale pending decision instead of rejecting it outright")
	}
}

// TestDrawStepSkipsEliminatedActivePlayer is the Ruling C regression test:
// the once-per-turn draw must check that the active player is still alive
// before drawing for them.
//
// Ruling T28-a (rewritten for Task 28): this used to force the scenario by
// calling e.priorityRound() directly against raw-forged state (a
// TurnChange/StepChange/Priority/PlayerLost sequence standing in for "turn 2,
// draw step, no passes yet, priority already on the active seat, that seat
// eliminated" -- exactly what priorityRound's own draw guard used to
// inspect). Task 28 deleted that guard: priorityRound no longer performs any
// draw at all, for any player, so calling it directly now passes this
// scenario VACUOUSLY -- there is no draw to skip because there is no draw
// there any more. The property this pins is still real (an eliminated active
// player must not draw once the check moves to advanceStep), so this now
// drives a genuine elimination through the real path: a player who dies to
// something other than drawing (here, a raw PlayerLost -- CR 704.5a covers
// the common real case, life loss, but the mechanism is agnostic to why)
// partway through their OWN turn, before that turn's draw step is reached.
func TestDrawStepSkipsEliminatedActivePlayer(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 4)
	driveToStep(t, e, 2, 1, state.StepUpkeep)

	// checkStateBased is what a real Submit would run next; it is what
	// actually resumes the round through the newly-eliminated seat (via
	// releasePendingDecisionOfDepartedPlayer) rather than leaving a stale
	// priority decision addressed to a player who can no longer answer it.
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "test"})
	e.checkStateBased()

	libBefore := len(e.G.Zone(state.ZLibrary, 1))
	firstNewEvent := len(e.L.Events)

	// Turn 2 (seat 1's own turn, already under way) still runs its steps in
	// full -- an eliminated active player is skipped for PRIORITY, not
	// exempted from the turn structure -- so its draw step is still entered.
	// Turn 3 goes to NextAlive(1) = seat 2, confirming the match kept moving.
	driveToStep(t, e, 3, 2, state.StepUpkeep)

	for _, ev := range e.L.Events[firstNewEvent:] {
		if ev.Kind == events.Draw && ev.Player == 1 {
			t.Fatalf("draw event emitted for eliminated active player 1")
		}
	}
	if got := len(e.G.Zone(state.ZLibrary, 1)); got != libBefore {
		t.Fatalf("library size for eliminated active player changed: %d -> %d", libBefore, got)
	}
}

// TestPlayLandReplayThroughSubmit is the Task 13 fix-round regression test
// for Ruling T13-a and T13-b as applied to handlePriority's play_land
// branch specifically: TestReplayReconstructsPassesAndPriority above only
// ever drives "pass" (via passAll/driveToOver), so it never exercised this
// branch's own Priority-reset emit or the LandPlayed emit at all.
//
// The action under test is driven through the public Submit path only --
// never handlePriority or legalActions directly.
//
// Two things make this test actually capable of catching a regression back
// to a direct field write, rather than passing either way:
//
//  1. Passes is forced to a nonzero value immediately before the action.
//     Entering a step naturally starts Passes at 0 (advanceStep's own emit
//     resets it), so testing straight off a step change could never tell
//     an emitted reset apart from a stale value that already happened to
//     be 0.
//  2. The other seat is eliminated immediately before the action, so this
//     Submit is the one that ends the game. priorityRound emits a fresh
//     Priority(holder, Passes) event before every decision it asks, and
//     Submit always calls Advance (which calls priorityRound again) right
//     after handling an intent -- so on any non-terminal action, that next
//     round's honest emit would re-broadcast the correct live values into
//     the log regardless of whether THIS action's own reset went through
//     emit or a bypassing direct write, silently curing the omission. Only
//     when no further priority round ever runs -- i.e. the action ends the
//     game -- does omitting the emit leave the log genuinely short a step,
//     which is what the replay comparison below is able to detect.
//
// This was verified empirically, not just reasoned through: see the
// Task 13 fix-round report for the revert-and-rerun check.
func TestPlayLandReplayThroughSubmit(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 6, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	o := e.G.AddObject(mtn, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected player 0's priority, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "play_land" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for the hand land: %+v", d.Options)
	}

	// Setup only (see point 1 and 2 above): neither of these is the action
	// under test.
	e.emit(events.Event{Kind: events.Priority, Player: 0, Amount: 1})
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "test"})

	// The action under test: a play_land intent through Submit.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}

	if !e.G.Over {
		t.Fatal("eliminating the only other seat must end the game on this Submit")
	}
	if got := e.G.Players[0].LandsPlayed; got != 1 {
		t.Fatalf("LandsPlayed = %d, want 1", got)
	}
	if got := o.Zone; got != state.ZBattlefield {
		t.Fatalf("land zone = %s, want battlefield", got)
	}
	if e.G.Passes != 0 {
		t.Fatalf("Passes = %d, want 0 after the land drop", e.G.Passes)
	}
	if e.G.Priority != 0 {
		t.Fatalf("Priority = %d, want to stay with the acting player (0)", e.G.Priority)
	}

	// Replay through events.Apply alone -- no Engine, no rules code -- and
	// confirm it reconstructs the state the two rulings govern.
	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}
	if fresh.Passes != e.G.Passes {
		t.Errorf("replayed Passes = %d, want %d (live)", fresh.Passes, e.G.Passes)
	}
	if fresh.Priority != e.G.Priority {
		t.Errorf("replayed Priority = %d, want %d (live)", fresh.Priority, e.G.Priority)
	}
	for p := state.PlayerID(0); p < 2; p++ {
		if fresh.Players[p].LandsPlayed != e.G.Players[p].LandsPlayed {
			t.Errorf("replayed player %d LandsPlayed = %d, want %d (live)",
				p, fresh.Players[p].LandsPlayed, e.G.Players[p].LandsPlayed)
		}
	}
}

// TestActivateManaAbilityReplayThroughSubmit is
// TestPlayLandReplayThroughSubmit's sibling for the activate branch: same
// two setup requirements (nonzero Passes beforehand, the action ends the
// game so no later priority round can paper over an omitted emit), same
// Submit-only path for the action under test, same replay comparison.
//
// The land is placed directly on the battlefield as setup (the same
// technique legal_test.go's handEngine uses for hand cards): genesis-style
// object placement is never logged (see the rules package doc comment), so
// replay is not expected to, and does not need to, reconstruct the land's
// zone or tapped state here -- only the scalar fields the two rulings
// govern (Passes, Priority, LandsPlayed).
//
// The pool DOES change here: the bare Mountain fixture has no printed
// A:AB$ Mana line, but cards/intrinsic.go's ApplyIntrinsics grants every
// basic land its subtype's tap ability regardless, so ManaAbilities()
// returns 1, legal.go's "activate" case finds it, resolveAbility runs, and
// one ManaAdd R 1 is emitted (0 -> 1) -- Submit below relies on exactly
// that: activateManaAbility's own t.Fatalf("no activate option...") would
// fire if it did not. This test simply does not ASSERT the pool change; it
// is about Passes/Priority/Tapped surviving Submit+replay, not mana.
func TestActivateManaAbilityReplayThroughSubmit(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b"}
	cfg := Config{Seed: 9, Names: names,
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZHand, p, nil)
	}
	mtn := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	o := e.G.AddObject(mtn, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	e.G.Step = state.StepMain1
	e.G.Active, e.G.Priority = 0, 0
	e.G.Turn = 1

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("expected player 0's priority, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == o.ID {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no activate option for the battlefield land: %+v", d.Options)
	}

	// Setup only: neither of these is the action under test.
	e.emit(events.Event{Kind: events.Priority, Player: 0, Amount: 1})
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "test"})

	// The action under test: an activate intent through Submit.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit activate: %v", err)
	}

	if !e.G.Over {
		t.Fatal("eliminating the only other seat must end the game on this Submit")
	}
	if !o.Tapped {
		t.Fatal("activating the mana ability should tap the land")
	}
	if e.G.Passes != 0 {
		t.Fatalf("Passes = %d, want 0 after activating", e.G.Passes)
	}
	if e.G.Priority != 0 {
		t.Fatalf("Priority = %d, want to stay with the acting player (0) after activating", e.G.Priority)
	}

	fresh := state.NewGame(cfg.Names)
	for _, ev := range e.L.Events {
		events.Apply(fresh, ev)
	}
	if fresh.Passes != e.G.Passes {
		t.Errorf("replayed Passes = %d, want %d (live)", fresh.Passes, e.G.Passes)
	}
	if fresh.Priority != e.G.Priority {
		t.Errorf("replayed Priority = %d, want %d (live)", fresh.Priority, e.G.Priority)
	}
}
