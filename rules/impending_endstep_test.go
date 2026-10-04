package rules

// The "your end step" half of CR 702.176a: the granted removal trigger must
// tick only on the CONTROLLER's end step (ValidPlayer$ You on the Mode$ Phase
// grant), never on an opponent's. This is the discriminating test for the
// trigger's player scope -- a grant that ticked on every end step would pass
// TestImpendingEndStepRemovesATimeCounterAndWakesAtTheLast (which only drives
// the controller's own end steps) and still be wrong.

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestImpendingDoesNotTickOnOpponentEndStep(t *testing.T) {
	t.Parallel()
	e, _ := impendingEngine(t, 9711)
	id := castImpending(t, e)
	if got := e.G.Obj(id).Counter("TIME"); got != 2 {
		t.Fatalf("precondition: entered with %d time counters, want 2", got)
	}
	// The controller (seat 0) end step of turn 1: must tick 2 -> 1.
	driveToStep(t, e, 1, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("after the controller's own end step: %d time counters, want 1", got)
	}
	// The opponent (seat 1) end step of turn 2: must NOT tick; stays 1.
	driveToStep(t, e, 2, 1, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(id).Counter("TIME"); got != 1 {
		t.Fatalf("after the opponent's end step: %d time counters, want 1 (only the controller's end step ticks)", got)
	}
}
