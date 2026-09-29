package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// A turn skipped between the controller's current and next actual turn has
// no TurnChange. Karn's UntilYourNextTurn animation therefore ends at the
// cleanup immediately BEFORE the next actual turn, not one cleanup later.
func TestSkipTurnReschedulesKarnNextTurnBoundary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
	}, nil)
	ring := animateKarnRing(t, e)
	if b := boundaryOf(t, e, ring); b != 2 {
		t.Fatalf("precondition: registered animation ends at turn %d, want 2 before skip", b)
	}
	// The late skip moves Karn's next actual turn from turn 3 to turn 2.
	e.emit(events.Event{Kind: events.SkipTurn, Player: 1, Amount: 1})
	if e.G.SkipTurns[1] != 1 {
		t.Fatal("precondition: opponent's turn was not marked for skipping")
	}
	if got := e.nextTurnFor(0); got != 2 {
		t.Fatalf("Karn's next actual turn = %d, want 2", got)
	}
	driveToStep(t, e, 2, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("animation survived into Karn's next actual turn after opponent skipped")
	}
	if e.G.SkipTurns[1] != 0 {
		t.Fatal("opponent's skipped turn was not consumed")
	}
	replayCheck(t, e, cfg)
}

// Skipping the controller's own next slot delays its next ACTUAL turn.
// The animation must outlive both opponent turns and expire before the
// controller finally begins turn 4.
func TestSkipTurnDelaysKarnNextTurnBoundary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{
		lookup(t, reg, "Karn, the Great Creator"), lookup(t, reg, "Sol Ring"),
	}, nil)
	ring := animateKarnRing(t, e)
	if b := boundaryOf(t, e, ring); b != 2 {
		t.Fatalf("precondition: registered animation boundary = %d, want 2", b)
	}
	e.emit(events.Event{Kind: events.SkipTurn, Player: 0, Amount: 1})
	if e.G.SkipTurns[0] != 1 {
		t.Fatal("precondition: controller's next turn was not marked for skipping")
	}
	if got := e.nextTurnFor(0); got != 4 {
		t.Fatalf("Karn's next actual turn = %d, want 4", got)
	}
	driveToStep(t, e, 2, 1, state.StepMain1)
	if !e.IsCreature(ring) {
		t.Fatal("animation expired on the first opponent turn")
	}
	driveToStep(t, e, 3, 1, state.StepMain1)
	if !e.IsCreature(ring) || e.G.SkipTurns[0] != 0 {
		t.Fatal("animation expired before skipped controller turn or skip not consumed")
	}
	driveToStep(t, e, 4, 0, state.StepMain1)
	if e.IsCreature(ring) {
		t.Fatal("animation survived into the controller's actual next turn")
	}
	replayCheck(t, e, cfg)
}

// An end-boundary control grant registered before a late skip must also end
// on its controller's next REAL turn, not the former numerical boundary.
func TestSkipTurnReschedulesControlNextTurnBoundary(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := corpusEngineCfg(t, reg, nil, nil)
	stolen := stealRealArtifact(t, e)
	if b := controlBoundaryOf(t, e, stolen); b != 3 {
		t.Fatalf("precondition: registered control boundary = %d, want 3", b)
	}
	e.emit(events.Event{Kind: events.SkipTurn, Player: 1, Amount: 1})
	driveToStep(t, e, 2, 0, state.StepMain1)
	if e.G.Obj(stolen).Controller != 0 {
		t.Fatal("control ended before its controller's next turn finished")
	}
	driveToStep(t, e, 3, 1, state.StepMain1)
	if e.G.Obj(stolen).Controller != 1 {
		t.Fatal("control survived its controller's actual next turn cleanup")
	}
}

// Queued skips consume counts before ordinary rotation: the skipped grant
// must not erase the same player's later normal turn, or any surviving grant.
func TestNextTurnForConsumesQueuedSkipCounts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := corpusEngineCfg(t, reg, nil, nil)
	e.emit(events.Event{Kind: events.SkipTurn, Player: 0, Amount: 1})
	e.emit(events.Event{Kind: events.ExtraTurn, Player: 0, Amount: 1})
	if len(e.G.ExtraTurnQueue) != 1 || e.G.SkipTurns[0] != 1 {
		t.Fatalf("precondition: queued extra turn and skip missing: %+v, %+v", e.G.ExtraTurnQueue, e.G.SkipTurns)
	}
	if got := e.nextTurnFor(0); got != 3 {
		t.Fatalf("after skipped extra turn, seat 0's ordinary next turn = %d, want 3", got)
	}
	driveToStep(t, e, 3, 0, state.StepMain1)
	if e.G.SkipTurns[0] != 0 || len(e.G.ExtraTurnQueue) != 0 {
		t.Fatal("queued skip and extra turn were not consumed")
	}
	replayCheck(t, e, cfg)
}
