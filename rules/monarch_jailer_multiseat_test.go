package rules

// Round-2 review regressions for CR 724 (the monarch) and Palace Jailer's
// command-zone return promise. Two defects the first round shipped:
//
//  1. Palace Jailer's `ValidPlayer$ Player.OpponentOf Remembered` was read as
//     "the new monarch controls the remembered exiled creature", which is the
//     wrong anchor. The relation is against the EFFECT's controller: seat 0's
//     Jailer exiles seat 1's creature, and ANY opponent of seat 0 (seat 2 in a
//     three-seat game) becoming the monarch must return it.
//  2. The synthetic monarch-draw DelayedPush carried Amount zero, which
//     events.Apply read as delayed-registration ID 0 and deleted -- dropping
//     the first unrelated delayed trigger whenever the monarch's draw went on
//     the stack.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestPalaceJailerReturnsToThirdSeatOpponentMonarch is the three-seat
// regression: the exiled creature belongs to seat 1, but seat 2 -- a
// DIFFERENT opponent of the Jailer's controller (seat 0) -- takes the crown.
// The creature must return.
func TestPalaceJailerReturnsToThirdSeatOpponentMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := newSeats(t, 3)
	jailer := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Palace Jailer"))
	victim := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))

	if e.G.Obj(victim).Controller != 1 {
		t.Fatal("precondition: seat 1 must control the exiled creature")
	}
	face := e.G.Obj(jailer).Face()
	body := cards.ResolveSVar(face.SVars, "DBEffect")
	if body == nil || body.API != "Effect" {
		t.Fatal("precondition: Palace Jailer DBEffect is missing")
	}
	// Resolve Palace Jailer's real Effect body (RememberObjects$ You &
	// Targeted) as its controller seat 0, remembering the creature it exiled.
	effects.Resolve(e, &effects.Ctx{Source: jailer, Controller: 0,
		Remembered: []state.Target{{Obj: victim}}, Captured: []state.Target{{Obj: victim}}}, body)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "BecomeMonarch" {
		t.Fatalf("Palace Jailer ComeBack registration = %+v, want one BecomeMonarch registration", e.G.Delayed)
	}
	if e.G.Delayed[0].Controller != 0 {
		t.Fatalf("registration controller = %d, want seat 0 (the jailer's controller)", e.G.Delayed[0].Controller)
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: victim,
		From: state.ZBattlefield, To: state.ZExile})
	if got := e.G.Obj(victim).Zone; got != state.ZExile {
		t.Fatalf("precondition: victim zone = %s, want exile", got)
	}

	// Seat 2 -- an opponent of seat 0, but NOT the exiled creature's own
	// controller -- becomes the monarch.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 2})
	if got := e.G.Obj(victim).Zone; got != state.ZBattlefield {
		t.Fatalf("victim zone = %s, want battlefield after an opponent of the jailer's controller became monarch", got)
	}
}

// TestPalaceJailerStaysExiledWhenJailerControllerTakesCrown is the negative
// control for the same relation: "until an OPPONENT becomes the monarch"
// must NOT return the creature when the Jailer's own controller takes the
// crown. Without this the positive test would pass for a blanket return.
func TestPalaceJailerStaysExiledWhenJailerControllerTakesCrown(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := newSeats(t, 3)
	jailer := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Palace Jailer"))
	victim := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))

	face := e.G.Obj(jailer).Face()
	body := cards.ResolveSVar(face.SVars, "DBEffect")
	if body == nil || body.API != "Effect" {
		t.Fatal("precondition: Palace Jailer DBEffect is missing")
	}
	effects.Resolve(e, &effects.Ctx{Source: jailer, Controller: 0,
		Remembered: []state.Target{{Obj: victim}}, Captured: []state.Target{{Obj: victim}}}, body)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "BecomeMonarch" {
		t.Fatalf("precondition: want one BecomeMonarch registration, got %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim,
		From: state.ZBattlefield, To: state.ZExile})

	// The jailer's own controller (seat 0) becomes the monarch: not an
	// opponent, so the creature stays exiled.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	if got := e.G.Obj(victim).Zone; got != state.ZExile {
		t.Fatalf("victim zone = %s, want exile (the jailer's controller is not an opponent)", got)
	}
}

// TestMonarchEndStepDrawDoesNotConsumeDelayedRegistration pins the second
// defect: the synthetic monarch-draw DelayedPush carries no card
// registration, so it must not consume the first (ID 0) one.
func TestMonarchEndStepDrawDoesNotConsumeDelayedRegistration(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := e.G.Zone(state.ZHand, 0)[0]

	// Register an ordinary delayed trigger. It is the first registration, so
	// it takes delayed ID 0 -- exactly what the monarch draw's zero Amount
	// used to collide with. A later Upkeep phase keeps it from firing during
	// this test's end step.
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0,
		Step: state.StepUpkeep, Counter: "TrigSomething"})
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].ID != 0 {
		t.Fatalf("precondition: registration = %+v, want one entry with ID 0", e.G.Delayed)
	}

	// The monarch's end step: seat 0 draws through the real triggered ability.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	if !e.G.IsMonarch(0) {
		t.Fatal("precondition: seat 0 did not become the monarch")
	}
	e.G.Step = state.StepEnd
	e.finishEnteredStep()
	if len(e.pendingTriggers) == 0 || !e.pendingTriggers[0].MonarchDraw {
		t.Fatalf("end step queued %+v, want a monarch draw trigger", e.pendingTriggers)
	}
	if e.putTriggersOnStack() || len(e.G.Stack) != 1 {
		t.Fatalf("monarch draw was not put on the stack: pending=%+v stack=%v", e.Pending(), e.G.Stack)
	}

	// The bystander registration must still be pending: the monarch draw has
	// no registration of its own to consume.
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].ID != 0 {
		t.Fatalf("monarch draw consumed the delayed registration: delayed=%+v, want the ID-0 entry intact", e.G.Delayed)
	}
}
