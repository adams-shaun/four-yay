package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMonarchEndStepDrawUsesTheStack(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	before := len(e.G.Zone(state.ZHand, 0))
	e.G.Step = state.StepEnd
	e.finishEnteredStep()
	if len(e.pendingTriggers) != 1 || !e.pendingTriggers[0].MonarchDraw {
		t.Fatalf("end step queued %+v, want monarch draw trigger", e.pendingTriggers)
	}
	if len(e.G.Zone(state.ZHand, 0)) != before {
		t.Fatal("monarch draw happened before its triggered ability resolved")
	}
	if e.putTriggersOnStack() || len(e.G.Stack) != 1 {
		t.Fatalf("monarch draw was not put on stack: pending=%+v stack=%v", e.Pending(), e.G.Stack)
	}
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("monarch hand size = %d, want %d", got, before+1)
	}
}

func TestCombatDamageToMonarchTransfersTheCrown(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	attacker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	if !e.G.IsMonarch(0) || e.G.Obj(attacker).Controller != 1 {
		t.Fatal("precondition: seat 0 must be monarch and seat 1 must control attacker")
	}
	e.combatRound.assignments = []assignment{{from: attacker, toPlayer: 0, amount: 2}}
	e.runCombatAssignments()
	if !e.G.IsMonarch(1) {
		t.Fatalf("combat damage did not transfer crown: monarch=%d", e.G.Monarch)
	}
}
