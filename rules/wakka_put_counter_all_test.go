package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestWakkaBlitzballCaptainPutsCountersOnOtherCreatures pins the real
// end-step trigger and its PutCounterAll body: Wakka must have received a
// counter this turn, then each other creature controlled by its controller
// gets one +1/+1 counter, but Wakka and the opponent's creature do not.
func TestWakkaBlitzballCaptainPutsCountersOnOtherCreatures(t *testing.T) {
	t.Parallel()
	wakkaCard := corpusCard(t, "Wakka, Devoted Guardian")
	e, cfg := putCounterTable(t, 20260923,
		[]*cards.Card{wakkaCard, card(t, counterBear("Blitzball Teammate"))},
		[]*cards.Card{card(t, counterBear("Opponent Teammate"))})
	wakka := findAndMoveToBattlefield(t, e, 0, "Wakka, Devoted Guardian")
	teammate := findAndMoveToBattlefield(t, e, 0, "Blitzball Teammate")
	opponent := findAndMoveToBattlefield(t, e, 1, "Opponent Teammate")
	if e.G.Obj(wakka).Zone != state.ZBattlefield || e.G.Obj(teammate).Zone != state.ZBattlefield || e.G.Obj(opponent).Zone != state.ZBattlefield {
		t.Fatal("test precondition: Wakka and both teammates must be on the battlefield")
	}

	// The trigger checks whether a counter was put on Wakka during this turn.
	previousAdder := e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: wakka, Counter: "P1P1", Amount: 1})
	e.SetCounterAdder(previousAdder)
	if got := e.G.Obj(wakka).Counter("P1P1"); got != 1 {
		t.Fatalf("test precondition: Wakka has %d counters after placement, want 1", got)
	}

	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	drainQueuedTriggers(t, e)
	if got := e.G.Obj(teammate).Counter("P1P1"); got != 1 {
		t.Fatalf("Wakka's end-step trigger gave teammate %d counters, want 1", got)
	}
	if got := e.G.Obj(wakka).Counter("P1P1"); got != 1 {
		t.Fatalf("StrictlyOther sweep changed Wakka's counters to %d, want 1", got)
	}
	if got := e.G.Obj(opponent).Counter("P1P1"); got != 0 {
		t.Fatalf("Wakka's controller gave opponent's creature %d counters, want 0", got)
	}
	if hasNote(e, "unimplemented API PutCounterAll") {
		t.Fatal("Wakka's PutCounterAll handler did not run")
	}
	if !hasCounterChange(e, teammate, "P1P1", 1) {
		t.Fatal("Wakka's end-step trigger emitted no CounterChange for its other creature")
	}
	replayCheck(t, e, cfg)
}
