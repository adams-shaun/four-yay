package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Task E1b — CR 800.4j: "If a player leaves the game during their turn, that
// turn continues to its completion without an active player. If the active
// player would receive priority, instead the next player in turn order
// receives priority, or the top object on the stack resolves, or the phase or
// step ends, whichever is appropriate."
//
// This task writes tests only. It pins the behaviors that make 800.4j true in
// gorge, and it exists so that a future edit which ends an eliminated active
// player's turn early (the parked-but-discarded endTurnForEliminatedActivePlayer
// approach, or any re-derivation of it) is caught by a named test rather than
// silently degrading real magic: the turn MUST continue, the departed seat MUST
// never be handed a decision, and a living seat's end-step trigger MUST still
// fire.
//
// The fixtures below are ours, fresh from the CR rather than ported from the
// parked branch (which used this file's name). drainerSrc/gainerSrc are reused
// from trigger_order_test.go because drainerSrc is exactly the repo's existing
// self-elimination fixture: it loses the controller's ENTIRE life total on an
// upkeep trigger, so a controller on their own turn is eliminated mid-turn.

// endGainSrc is a living seat's end-of-turn trigger (behaviour 4's fixture),
// modelled on gainerSrc/drainerSrc: it gains its controller 5 life "at the
// beginning of the end step". On a correctly-continuing turn an eliminated
// active player's turn still HAS an end step, so a living seat's trigger
// still fires there.
//
// Phase$ matching goes through state.ParsePhases (the one Forge phase-name
// parser), so `Phase$ End of Turn` resolves at exactly the end step -- the
// name Forge's PhaseType.smartValueOf resolves. (An earlier draft wrote the
// bare `Phase$ End` and relied on the substring tolerance the old parser
// had; bare "End" is not a Forge script name, so the shared parser reports
// it and the trigger never fires.) A correctly-completed turn fires this
// trigger once, gaining exactly 5; the assertion below pins that exact
// delta, which is what a turn-ending mutation removes and this test pins.
const endGainSrc = `Name:Endgainer
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ End of Turn | Execute$ TrigEndGain | TriggerDescription$ gain 5 life
SVar:TrigEndGain:DB$ GainLife | LifeAmount$ 5 | Defined$ You
Oracle:x
`

// elimActiveScenario builds a three-seat game in which seat 1 is eliminated
// mid-turn on their OWN turn 2, at upkeep, by a self-drain trigger, while a
// LIVING seat (2) carries a Phase-End trigger that must still fire on seat 1's
// continuing turn. Returns the engine parked at turn 2's upkeep: seat 1 still
// alive, the drain trigger already on the stack, and a priority decision for
// seat 1 pending (which is what the writer will then pass through the rest of
// the turn).
//
// seat 1 is chosen so a single driveToStep reaches the elimination cleanly:
// turn order is 0,1,2, so turn 2 belongs to seat 1 and its own upkeep is the
// first trigger window after the fixtures are placed (they are placed after
// genesis, so they miss the already-past turn 1 upkeep). seat 1 eliminates
// itself; seat 2 is the survivor that must reach the next turn while keeping
// its end-step trigger alive. Mountains all round, so combat auto-skips.
func elimActiveScenario(t *testing.T) *Engine {
	t.Helper()
	e := newSeats(t, 3)
	onBoard(t, e, 1, drainerSrc) // controller seat 1: drains its whole life at upkeep
	onBoard(t, e, 2, endGainSrc) // living seat 2: end-step trigger
	driveToStep(t, e, 2, 1, state.StepUpkeep)
	// Prologue must hold for the rest of these assertions to mean anything.
	if e.G.Players[1].Lost {
		t.Fatal("setup: seat 1 was eliminated before its own turn 2 upkeep")
	}
	if len(e.G.Stack) != 1 {
		t.Fatalf("setup: stack = %v, want the drain trigger already placed", e.G.Stack)
	}
	return e
}

// playEliminatedTurn passes every priority decision until the engine reaches
// the given turn boundary, failing on any non-priority decision (there should
// be none: mountains never attack, and every trigger on the board is a lone
// mandatory one). It refuses to drive past an Over game and asserts the match
// is still playable when it stops.
func playEliminatedTurn(t *testing.T, e *Engine, untilTurn int32) {
	t.Helper()
	for n := 0; n < 800 && !e.G.Over; n++ {
		if e.G.Turn >= untilTurn {
			return
		}
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v appeared while playing through the eliminated turn", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit %s %d: %v", d.Kind, d.Player, err)
		}
	}
	if !e.G.Over && e.Pending() == nil {
		t.Fatal("the match stalled: neither over nor anything pending")
	}
}

// indexOfPlayerLost returns the log index of the first PlayerLost event for
// seat p, or -1 if none.
func indexOfPlayerLost(e *Engine, p state.PlayerID) int {
	for i, ev := range e.L.Events {
		if ev.Kind == events.PlayerLost && ev.Player == p {
			return i
		}
	}
	return -1
}

// indexOfTurnChange returns the log index of the first TurnChange event whose
// Amount (the new turn number) equals wantTurn, or -1 if none.
func indexOfTurnChange(e *Engine, wantTurn int32) int {
	for i, ev := range e.L.Events {
		if ev.Kind == events.TurnChange && ev.Amount == wantTurn {
			return i
		}
	}
	return -1
}

// steppedInto reports whether the open interval (from,to) of the log contains
// a StepChange event to step s.
func steppedInto(e *Engine, from, to int, s state.Step) bool {
	if to < 0 {
		to = len(e.L.Events) - 1
	}
	for i := from + 1; i < to; i++ {
		if e.L.Events[i].Kind == events.StepChange && e.L.Events[i].Step == s {
			return true
		}
	}
	return false
}

// --- Behaviour 1: the turn continues ----------------------------------------

// TestEliminatedActivePlayersTurnContinuesToCleanup is behaviour 1. The active
// player is eliminated mid-turn (their own self-drain upkeep); the turn must
// still advance through its remaining steps to cleanup, then hand to the next
// turn. Asserted on the real event log: between seat 1's PlayerLost and the
// TurnChange that opens turn 3, the engine must have entered both the end step
// and the cleanup step. A stronger-than-Magic rule that ended the turn at the
// elimination would skip both, and this named test would fail.
func TestEliminatedActivePlayersTurnContinuesToCleanup(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	playEliminatedTurn(t, e, 3)
	// The elimination happens during playEliminatedTurn (the drain trigger
	// resolves partway through), so the PlayerLost and the turn-end markers
	// are found in the log only after the turn has been played out.
	lost := indexOfPlayerLost(e, 1)
	if lost < 0 {
		t.Fatal("seat 1 was never eliminated")
	}
	endOfTurn := indexOfTurnChange(e, 3)
	if endOfTurn < 0 {
		t.Fatal("the turn never became turn 3")
	}
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3 — the eliminated player's turn did not complete", e.G.Turn)
	}
	if !steppedInto(e, lost, endOfTurn, state.StepEnd) {
		t.Fatal("the eliminated player's turn never entered the end step")
	}
	if !steppedInto(e, lost, endOfTurn, state.StepCleanup) {
		t.Fatal("the eliminated player's turn never entered cleanup — a stronger-than-Magic rule stopped it early")
	}
}

// --- Behaviour 2: no priority to the departed seat --------------------------

// TestNoPriorityIsGrantedToTheEliminatedActivePlayer is behaviour 2. CR
// 800.4j's pivot is "if the active player would receive priority, instead the
// next player in turn order receives priority": what must never happen is the
// departed seat being handed the priority DECISION. We assert on the asks,
// not on a guard: across the whole log from the elimination onward there must
// be no DecisionAsk asking seat 1 to take a priority action.
//
// (A separate defect — advanceStep emitting a bare events.Priority naming the
// still-nominal active seat on each step transition of the continuing turn —
// is reported loudly in the dispatch report's cover note and NOT fixed here;
// this task adds no production code. That bare event carries no DecisionAsk
// behind it: grantPriority re-points the actual grant to NextAlive, so the
// departed seat is never asked to act. This test pins the decision-level
// guarantee — the thing a client asking "do I need a live response from seat
// 1?" cares about — which is what must hold for the match to keep moving.)
func TestNoPriorityIsGrantedToTheEliminatedActivePlayer(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	before := len(e.L.Events)
	playEliminatedTurn(t, e, 3)
	lost := indexOfPlayerLost(e, 1)
	if lost < 0 {
		t.Fatal("seat 1 was never eliminated")
	}
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3 (so the assertions below are about a full continuing turn)", e.G.Turn)
	}
	for i := lost; i < len(e.L.Events); i++ {
		ev := e.L.Events[i]
		if ev.Kind == events.DecisionAsk && ev.Player == 1 && ev.Text == "priority" {
			t.Fatalf("log[%d]: a priority decision (%q) was asked of departed seat 1 — CR 800.4j requires it go to the next seat", i, ev.Text)
		}
	}
	if got := len(e.L.Events); got == before {
		t.Fatal("control: the log did not grow during the eliminated turn")
	}
}

// --- Behaviour 3: no decision of any kind to the departed seat --------------

// TestNoDecisionIsEverAskedOfTheDepartedSeat is behaviour 3, over the whole
// turn and every decision kind. An eliminated seat must not be asked to choose
// attackers, blockers, targets, trigger orders or anything else for the rest
// of the turn — attackers included: an eliminated seat's battlefield is swept
// by checkLoseConditions, so askAttackers finds it empty and falls to end of
// combat rather than handing a departed seat a decision. Asserted on the asks
// across the whole log from the elimination, not on any one guard.
func TestNoDecisionIsEverAskedOfTheDepartedSeat(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	playEliminatedTurn(t, e, 3)
	lost := indexOfPlayerLost(e, 1)
	if lost < 0 {
		t.Fatal("seat 1 was never eliminated")
	}
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3 (so the assertions span the full continuing turn)", e.G.Turn)
	}
	// Scoped to the log from the elimination onward: prior to it, seat 1 was a
	// wholly ordinary, living seat and legitimately answered priority questions.
	log := e.L.Events[lost:]
	for _, ev := range log {
		if ev.Kind == events.DecisionAsk && ev.Player == 1 {
			t.Fatalf("a %q decision was asked of departed seat 1 — no decision may be asked of a departed seat", ev.Text)
		}
	}
	// The eliminated seat also never declared attackers on this (their own,
	// eliminated) turn: no attacker decision names them at all.
	for _, ev := range log {
		if ev.Kind == events.DeclareAttackers && ev.Player == 1 {
			t.Fatal("departed seat 1 declared attackers on their own eliminated turn")
		}
	}
}

// --- Behaviour 4: a living seat's end-step trigger still fires ----------------

// TestLivingSeatsEndOfTurnTriggerStillFiresOnTheEliminatedTurn is behaviour 4,
// the divergence tripwire. A living seat (2) carries a Phase-End trigger. If a
// future edit ends an eliminated active player's turn early, that eliminates
// the end of the turn too, and seat 2's trigger silently stops firing. On the
// correct 800.4j continuation the end-combat and end steps still run, so seat
// 2's Phase-End trigger resolves (gaining 5 life per firing) during the
// eliminated player's turn. Asserted on the real life delta, not on a trigger
// queued/still-piled internal.
func TestLivingSeatsEndOfTurnTriggerStillFiresOnTheEliminatedTurn(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	life := e.G.Players[2].Life
	playEliminatedTurn(t, e, 3)
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3", e.G.Turn)
	}
	// Phase$ End of Turn fires at exactly the end step, so a completed turn
	// grants exactly one 5-life firing. The tripwire is that seat 2 gained it
	// during the eliminated turn; ending the turn at the elimination removes
	// it and this fails.
	if delta := e.G.Players[2].Life - life; delta != 5 {
		t.Fatalf("seat 2 life delta across the eliminated turn = %d, want the one 5-life firing of its end-of-turn trigger — "+
			"the living seat's trigger did not fire on the eliminated player's turn", delta)
	}
}

// --- Behaviour 5: the next turn belongs to the next living seat --------------

// TestNextTurnBelongsToTheNextLivingSeat is behaviour 5. After the eliminated
// active player's turn completes, cleanup hands to NextAlive(Active) — the
// next seat still in the game in APNAP order, which after eliminating seat 1
// is seat 2 — and that turn begins normally, with a priority decision offered
// to seat 2.
func TestNextTurnBelongsToTheNextLivingSeat(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	playEliminatedTurn(t, e, 3)
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3", e.G.Turn)
	}
	if e.G.Active != 2 {
		t.Fatalf("active = %d, want 2 — the next living seat in APNAP order after eliminated seat 1", e.G.Active)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 2 {
		t.Fatalf("pending = %+v, want a turn-3 priority decision for seat 2 (the new turn must begin normally)", d)
	}
}

// --- Behaviour 6: a three-seat game does not end on one mid-turn elimination --

// TestThreeSeatGameContinuesWhenOnePlayerIsEliminatedMidTurn is behaviour 6.
// With three seats, eliminating the active player mid-turn does not end the
// game (two seats survive) — the turn must still complete and the match keep
// going.
func TestThreeSeatGameContinuesWhenOnePlayerIsEliminatedMidTurn(t *testing.T) {
	t.Parallel()
	e := elimActiveScenario(t)
	playEliminatedTurn(t, e, 3)
	if e.G.Over {
		t.Fatal("a three-seat game ended when one player was eliminated mid-turn")
	}
	if !e.G.Players[1].Lost {
		t.Fatal("seat 1 was not marked lost by the mid-turn elimination")
	}
	if e.G.Turn != 3 {
		t.Fatalf("turn = %d, want 3 — the game stopped advancing", e.G.Turn)
	}
}

// --- Behaviour 7: elimination that ends the game does not advance the turn ---

// TestEliminationThatEndsTheGameDoesNotAdvanceTheTurn is behaviour 7. In a
// two-seat game the active player's self-drain elimination leaves no opponent
// alive, so CR 104.4a ends the game outright: Over wins and the turn does not
// advance — no TurnChange (and no further StepChange/priority) may appear
// after the GameOver event.
func TestEliminationThatEndsTheGameDoesNotAdvanceTheTurn(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	onBoard(t, e, 1, drainerSrc)
	driveToStep(t, e, 2, 1, state.StepUpkeep)
	if e.G.Players[1].Lost {
		t.Fatal("setup: seat 1 eliminated before its own turn 2 upkeep")
	}
	playEliminatedTurn(t, e, 99)
	if !e.G.Players[1].Lost {
		t.Fatal("seat 1 was not eliminated by its own drain")
	}
	if !e.G.Over {
		t.Fatal("a two-seat game must end when the only opponent is eliminated mid-turn")
	}
	overIdx := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.GameOver {
			overIdx = i
			break
		}
	}
	if overIdx < 0 {
		t.Fatal("e.G.Over is true but no GameOver event was logged")
	}
	for _, ev := range e.L.Events[overIdx+1:] {
		if ev.Kind == events.TurnChange || ev.Kind == events.StepChange {
			t.Fatalf("a finished game advanced the turn/step: %s", ev.Kind)
		}
		if ev.Kind == events.Priority {
			t.Fatalf("a finished game granted priority: %s", ev.Kind)
		}
	}
}
