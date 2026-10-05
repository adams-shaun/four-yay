// Count$FinishedEndOfTurnsThisTurn — the per-turn end-step count head
// (ticket cli-20261005T092134Z-fed167ca, part 1). FIN Y'shtola Rhul grants an
// additional end step only when the resolving end step is the first of the
// turn: `SVar:X:Count$FinishedEndOfTurnsThisTurn` with a `ConditionSVarCompare$
// LT1` gate on DB$ AddPhase. Forge answers
// getNumEndOfTurn() - (is(END_OF_TURN) ? 1 : 0).
//
// This engine's counter is state.Game.EndStepsThisTurn, folded from the
// StepChange event (one increment per end-step ENTRY, so an AddPhase-spliced
// extra end step counts again) and reset at TurnChange. The pin drives the
// real folds and asserts the head through a first end step (0), a second end
// step in the same turn (1), and after the turn boundary (0).
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestFinishedEndOfTurnsThisTurnHeadOnYshthola pins Forge's
// Count$FinishedEndOfTurnsThisTurn against Y'shtola Rhul's real SVar X.
func TestFinishedEndOfTurnsThisTurnHeadOnYshthola(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	ysh := onBoardCard(t, e, 0, corpusCard(t, "Y'shtola Rhul"))
	face := e.G.Obj(ysh).Face()

	body, ok := face.SVars["X"]
	if !ok || body != "Count$FinishedEndOfTurnsThisTurn" {
		t.Fatalf("test precondition: Y'shtola SVar X = %q (ok %v)", body, ok)
	}
	ctx := &effects.Ctx{Controller: 0, Source: ysh}

	// Outside an end step the head reads the finished count directly; before
	// any end step that is 0. Assert the engine agrees the walk is not in an
	// end step so the read is unambiguous.
	if e.G.Step == state.StepEnd {
		t.Fatalf("precondition: engine already in the end step (%s)", e.G.Step)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("FinishedEndOfTurnsThisTurn before any end step = %d (ok %v), want 0", n, ok)
	}

	// Reach the first end step: one entry, minus the in-step adjustment = 0.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if e.G.Step != state.StepEnd {
		t.Fatalf("precondition: StepChange did not reach the end step (%s)", e.G.Step)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("FinishedEndOfTurnsThisTurn in the first end step = %d (ok %v), want 0", n, ok)
	}

	// An api:AddPhase-spliced extra end step in the same turn: the walk
	// leaves and re-enters the end step, so the count is 2 - 1 = 1.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("FinishedEndOfTurnsThisTurn in the second end step = %d (ok %v), want 1", n, ok)
	}

	// The turn boundary resets the per-turn fact.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	if e.G.EndStepsThisTurn != 0 {
		t.Fatalf("precondition: TurnChange did not reset EndStepsThisTurn (%d)", e.G.EndStepsThisTurn)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("FinishedEndOfTurnsThisTurn after the turn boundary = %d (ok %v), want 0", n, ok)
	}
}
