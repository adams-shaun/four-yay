package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the review round on the UntilYourNextTurn animate lifetime:
// the prior round only pinned extra turns already QUEUED at registration
// (AddContinuous's nextTurnFor walk). These two pin the other half -- a +1
// ExtraTurn grant emitted AFTER Karn's +1 resolved, which the frozen
// registration-time UntilTurn cannot see. EndOfTurnCleanup reschedules the
// boundary from the live rotation and queue before the expiry walk, so the
// animation must track the controller's next ACTUAL turn in both directions:
// the controller's own late grant moves the boundary EARLIER (the extra turn
// is the next turn -- the animation ends as it begins), another seat's late
// grant moves it LATER (the animation outlives the inserted turns).

// TestKarnAnimateDropsBeforeLateKarnExtraTurn: animate on turn 1 with no
// pending grants, then grant Karn's controller an extra turn. The granted
// turn is turn 2 and IS Karn's next turn, so the animation ends at turn 1's
// cleanup and must not be active during it.
func TestKarnAnimateDropsBeforeLateKarnExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
	}, nil)
	karn := moveByName(t, e, 0, "Karn, the Great Creator", state.ZBattlefield)
	ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	if e.IsCreature(ring) || e.G.Obj(karn).Counter("LOYALTY") <= 0 {
		t.Fatal("precondition: Karn must have loyalty and Sol Ring must be a noncreature")
	}
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, karn, 0).Index)
	submitTarget(t, e, ring)
	passUntilStackEmpty(t, e, 40)
	if !e.IsCreature(ring) {
		t.Fatal("precondition: Karn's +1 did not animate Sol Ring")
	}

	// The late grant: the extra turn is inserted AFTER the registration. The
	// registration-time boundary is still frozen at 2 here (the reschedule
	// runs at turn 1's cleanup) -- pin it so a future change to the
	// registration arithmetic shows up loudly.
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 0, Amount: 1})
	if b := boundaryOf(t, e, ring); b != 2 {
		t.Fatalf("registration-time animation boundary = turn %d, want 2", b)
	}

	// Turn 2 is Karn's extra turn (granted last, taken first); the
	// animation ended at turn 1's cleanup, as its turn begins.
	driveToStep(t, e, 2, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("animation survived into Karn's late-granted extra turn")
	}
}

// TestKarnAnimateOutlivesLateOpponentExtraTurn: animate on turn 1 with no
// pending grants, then grant the OPPONENT an extra turn. The inserted turns
// (opponent extra, opponent normal) precede Karn's next turn, so the
// animation must stay active through both and end only before Karn's turn 4.
func TestKarnAnimateOutlivesLateOpponentExtraTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
	}, nil)
	karn := moveByName(t, e, 0, "Karn, the Great Creator", state.ZBattlefield)
	ring := moveByName(t, e, 0, "Sol Ring", state.ZBattlefield)
	if e.IsCreature(ring) || e.G.Obj(karn).Counter("LOYALTY") <= 0 {
		t.Fatal("precondition: Karn must have loyalty and Sol Ring must be a noncreature")
	}
	e.pending = nil
	e.priorityRound()
	submitChoices(t, e, abilityOption(t, e, karn, 0).Index)
	submitTarget(t, e, ring)
	passUntilStackEmpty(t, e, 40)
	if !e.IsCreature(ring) {
		t.Fatal("precondition: Karn's +1 did not animate Sol Ring")
	}

	// The registration-time boundary is still frozen at 2 here (the
	// reschedule runs at turn 1's cleanup) -- pin it.
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 1, Amount: 1})
	if b := boundaryOf(t, e, ring); b != 2 {
		t.Fatalf("registration-time animation boundary = turn %d, want 2", b)
	}

	driveToStep(t, e, 2, 1, state.StepMain1)
	if !e.IsCreature(ring) {
		t.Fatal("animation did not survive the opponent's late-granted extra turn")
	}
	driveToStep(t, e, 3, 1, state.StepMain1)
	if !e.IsCreature(ring) {
		t.Fatal("animation did not survive the opponent's normal turn after their extra turn")
	}
	driveToStep(t, e, 4, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("animation survived into Karn's next turn after the inserted turns")
	}
}

// boundaryOf reads the expiry turn the animation's UntilYourNextTurn effect
// currently carries -- the registration-time value before any cleanup, the
// rescheduled value once EndOfTurnCleanup has run for the turn.
func boundaryOf(t *testing.T, e *Engine, ring state.ObjID) int32 {
	t.Helper()
	for _, ce := range e.continuous {
		if ce.Source == ring && ce.Controller == 0 && ce.Duration == "UntilYourNextTurn" {
			return ce.UntilTurn
		}
	}
	t.Fatal("precondition: Karn's +1 registered no UntilYourNextTurn effect on the animated artifact")
	return 0
}

// TestRescheduleNextTurnBoundariesEndSpellingCurrentTurn pins the one case
// the strictly-after walk cannot see: an UntilTheEndOfYourNextTurn effect
// whose boundary turn is the turn now being cleaned up must KEEP it --
// nextTurnFor never returns the turn in progress, so a plain recompute would
// push the boundary past it and the effect would survive its own expiry.
// Corpus-unreachable today (every Duration$ UntilTheEndOfYourNextTurn
// carrier -- measured 104 files -- is a DB$ Effect MayPlay shape the Effect
// registration gate does not admit), so this is a synthetic unit on
// rescheduleNextTurnBoundaries itself.
func TestRescheduleNextTurnBoundariesEndSpellingCurrentTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, nil, nil)
	e.continuous = append(e.continuous,
		ContinuousEffect{Duration: "UntilTheEndOfYourNextTurn", UntilTurn: 3, Controller: 0})

	// Turn 1, controller active, boundary strictly future: the recompute
	// keeps the frozen registration value (no extra turns pending; the
	// controller's next turn is turn 3).
	e.rescheduleNextTurnBoundaries()
	if e.continuous[0].UntilTurn != 3 {
		t.Fatalf("future boundary rescheduled to %d, want 3", e.continuous[0].UntilTurn)
	}

	// A late opponent grant inserts two turns before the boundary (opponent
	// extra turn 2, opponent normal turn 3), pushing it to turn 4.
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 1, Amount: 1})
	e.rescheduleNextTurnBoundaries()
	if e.continuous[0].UntilTurn != 4 {
		t.Fatalf("boundary after the late opponent grant = %d, want 4", e.continuous[0].UntilTurn)
	}

	// The boundary turn itself (turn 4, controller active): the override
	// keeps the boundary at the turn now ending instead of pushing it past.
	e.G.Active, e.G.Turn = 0, 4
	e.rescheduleNextTurnBoundaries()
	if e.continuous[0].UntilTurn != 4 {
		t.Fatalf("current-turn boundary rescheduled to %d, want 4", e.continuous[0].UntilTurn)
	}
}
