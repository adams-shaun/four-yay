package rules

// trig-become-monarch: Mode$ BecomeMonarch ("whenever a player becomes the
// monarch") was unregistered -- the api:BecomeMonarch primitive and the
// events.MonarchChange fold were real, but triggerMatches had no arm for the
// mode, so the five corpus carriers (Knights of the Black Rose, Custodi Lich,
// Garland Royal Kidnapper, Starscream Power Hungry and Palace Jailer's
// command-zone SVar) never fired. These leaves pin the mode on the real
// corpus SA -- Knights of the Black Rose, whose trigger carries both
// ValidPlayer$ Opponent and the BeginTurn$ You intervening-if ("if you were
// the monarch as the turn began") -- including the turn-start snapshot the
// gate reads.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// resolveKnightsETB drains the pending ETB trigger (the ChangesZone into
// Activate$ TrigMonarch -> DB$ BecomeMonarch) so seat 0 is the monarch. It
// asserts the trigger really was queued, so a silently-inert ETB cannot make
// a later assertion vacuously pass.
func resolveKnightsETB(t *testing.T, e *Engine) {
	t.Helper()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("Knights ETB queued %d triggers, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if e.Pending() != nil {
		t.Fatalf("Knights ETB left a pending decision: %+v", e.Pending())
	}
}

// TestKnightsOfTheBlackRoseDrainsWhenOpponentTakesTheCrownAfterYourTurn pins
// the positive half: seat 0's Knights enters (making seat 0 the monarch),
// seat 0 begins a later turn as the monarch, and an opponent then becomes the
// monarch -- the trigger fires, that opponent loses 2 life and seat 0 gains
// 2.
func TestKnightsOfTheBlackRoseDrainsWhenOpponentTakesTheCrownAfterYourTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Knights of the Black Rose"}, nil)

	// Precondition: the board starts with no monarch, so the ETB is what
	// makes seat 0 the monarch -- not genesis state.
	if e.G.HasMonarch {
		t.Fatalf("precondition: game began with a monarch (%d)", e.G.Monarch)
	}
	resolveKnightsETB(t, e)
	if !e.G.IsMonarch(0) {
		t.Fatalf("Knights ETB did not make seat 0 the monarch: HasMonarch=%v Monarch=%d",
			e.G.HasMonarch, e.G.Monarch)
	}

	// Seat 0 begins a fresh turn WHILE holding the crown: the TurnChange
	// snapshot records seat 0 as the turn-start monarch.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	if !e.G.WasMonarchAtTurnStart(0) {
		t.Fatalf("turn-start snapshot = %d (has=%v), want seat 0",
			e.G.TurnStartMonarch, e.G.HasTurnStartMonarch)
	}

	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	// The opponent takes the crown. This is the event the trigger matches.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	requireOneEventTrigger(t, e, "Knights of the Black Rose")
	e.putTriggersOnStack()
	e.resolveTop()

	if got := e.G.Players[1].Life; got != life1-2 {
		t.Fatalf("opponent life = %d, want %d (lost 2)", got, life1-2)
	}
	if got := e.G.Players[0].Life; got != life0+2 {
		t.Fatalf("seat 0 life = %d, want %d (gained 2)", got, life0+2)
	}
}

// TestKnightsOfTheBlackRoseSilentWhenTurnBeganWithoutTheCrown pins the
// BeginTurn$ You intervening-if: seat 0 takes the crown MID-turn (so it did
// NOT hold it as the turn began), and an opponent then becomes the monarch in
// that same turn -- the trigger must stay silent even though ValidPlayer$
// Opponent is satisfied.
func TestKnightsOfTheBlackRoseSilentWhenTurnBeganWithoutTheCrown(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Knights of the Black Rose"))

	// Seat 0 becomes the monarch mid-turn via the ETB's own effect.
	if e.G.HasMonarch {
		t.Fatalf("precondition: game began with a monarch (%d)", e.G.Monarch)
	}
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	if !e.G.IsMonarch(0) {
		t.Fatal("precondition: seat 0 did not become the monarch")
	}
	if e.G.WasMonarchAtTurnStart(0) {
		t.Fatal("precondition: the turn-start snapshot must not be seat 0 (the crown was taken mid-turn)")
	}

	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("BeginTurn$ You gate ignored: %d trigger(s) fired on a turn seat 0 did not begin as monarch",
			len(e.pendingTriggers))
	}
	if e.G.Players[0].Life != life0 || e.G.Players[1].Life != life1 {
		t.Fatalf("no trigger should have fired, but life moved: seat0 %d->%d, seat1 %d->%d",
			life0, e.G.Players[0].Life, life1, e.G.Players[1].Life)
	}
}

// TestKnightsOfTheBlackRoseSilentWhenSelfBecomesMonarch pins ValidPlayer$
// Opponent: seat 0 taking the crown itself ("You") must not match the
// opponent-scoped trigger -- neither from the trigger's own controller nor
// from the opponent's seat.
func TestKnightsOfTheBlackRoseSilentWhenSelfBecomesMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Knights of the Black Rose"}, nil)
	resolveKnightsETB(t, e)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})

	// Give the crown to seat 1 first (fires once), then take it back: the
	// take-back is seat 0 becoming the monarch, which ValidPlayer$ Opponent
	// must reject.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	requireOneEventTrigger(t, e, "Knights of the Black Rose")
	e.putTriggersOnStack()
	e.resolveTop()
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("ValidPlayer$ Opponent ignored: %d trigger(s) fired when the trigger's controller became the monarch",
			len(e.pendingTriggers))
	}
}

// TestCustodiLichRepeatedBecomeMonarchDoesNotFireSelfTrigger is the
// regression for the review MAJOR: an unconditional MonarchChange emit made
// a repeat DB$ BecomeMonarch on the REIGNING monarch fire "whenever you
// become the monarch" again. The mode fires on a TRANSITION (CR 720.2), so
// effBecomeMonarch now suppresses the event when its target already holds the
// designation. The pin drives the real corpus Custodi Lich SA: seat 0 is
// already the monarch, then the effect primitive is resolved on seat 0 a
// second time and its ValidPlayer$ You trigger must not queue.
func TestCustodiLichRepeatedBecomeMonarchDoesNotFireSelfTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	lich := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Custodi Lich"))

	// Precondition: seat 0 must already hold the crown, or the repeat is a
	// real first transition and the trigger SHOULD fire -- the assertion
	// below would then be vacuous. The establishing transition legitimately
	// fires Custodi Lich's own trigger (pinned end to end by
	// TestCustodiLichFirstBecomeMonarchFiresSelfTrigger), so drain it here.
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	requireOneEventTrigger(t, e, "Custodi Lich")
	e.pendingTriggers = nil
	if !e.G.IsMonarch(0) {
		t.Fatalf("precondition: seat 0 is not the monarch (has=%v monarch=%d)",
			e.G.HasMonarch, e.G.Monarch)
	}

	face := e.G.Obj(lich).Face()
	sa := cards.ResolveSVar(face.SVars, "TrigMonarch")
	if sa == nil {
		t.Fatal("precondition: Custodi Lich's TrigMonarch SVar did not resolve")
	}
	// Resolve the real DB$ BecomeMonarch body naming seat 0: the reigning
	// monarch. Before the fix this emitted MonarchChange and queued the
	// ValidPlayer$ You trigger.
	effects.Resolve(e, &effects.Ctx{Source: lich, Controller: 0}, sa)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a repeat BecomeMonarch on the reigning monarch queued %d trigger(s), want 0",
			len(e.pendingTriggers))
	}
}

// TestCustodiLichFirstBecomeMonarchFiresSelfTrigger proves the guard above is
// a TRANSITION test, not a blanket suppression: named a seat that does NOT
// hold the crown, the same SA emits the event and queues the trigger. Without
// this control the regression above would pass with the whole mode dead.
func TestPalaceJailerComeBackRegistersAndFiresOnOpponentMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	jailer := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Palace Jailer"))
	victim := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	face := e.G.Obj(jailer).Face()
	body := cards.ResolveSVar(face.SVars, "DBEffect")
	if body == nil || body.API != "Effect" {
		t.Fatal("precondition: Palace Jailer DBEffect is missing")
	}
	// The real ETB effect's remembered target is the creature it exiled.
	effects.Resolve(e, &effects.Ctx{Source: jailer, Controller: 0,
		Remembered: []state.Target{{Obj: victim}}, Captured: []state.Target{{Obj: victim}}}, body)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "BecomeMonarch" {
		t.Fatalf("Palace Jailer ComeBack registration = %+v, want one BecomeMonarch registration", e.G.Delayed)
	}
	if e.G.IsMonarch(1) {
		t.Fatal("precondition: opponent already held the monarch designation")
	}
	// Complete Palace Jailer’s real ETB consequence: the remembered opponent
	// creature is in exile when the opponent later takes the crown.
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim,
		From: state.ZBattlefield, To: state.ZExile})
	if got := e.G.Obj(victim).Zone; got != state.ZExile {
		t.Fatalf("precondition: Palace Jailer victim zone = %s, want exile", got)
	}

	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	if got := e.G.Obj(victim).Zone; got != state.ZBattlefield {
		t.Fatalf("Palace Jailer victim zone = %s, want battlefield after opponent becomes monarch", got)
	}
}

func TestCustodiLichFirstBecomeMonarchFiresSelfTrigger(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	lich := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Custodi Lich"))

	if e.G.IsMonarch(0) {
		t.Fatal("precondition: seat 0 already holds the crown")
	}
	face := e.G.Obj(lich).Face()
	sa := cards.ResolveSVar(face.SVars, "TrigMonarch")
	if sa == nil {
		t.Fatal("precondition: Custodi Lich's TrigMonarch SVar did not resolve")
	}
	effects.Resolve(e, &effects.Ctx{Source: lich, Controller: 0}, sa)
	if !e.G.IsMonarch(0) {
		t.Fatal("the first BecomeMonarch did not make seat 0 the monarch")
	}
	requireOneEventTrigger(t, e, "Custodi Lich")
}

// TestKnightsOfTheBlackRoseDrainReadsTriggeredPlayer pins the pg2 role the
// body reads: Defined$ TriggeredPlayer must resolve the NEW monarch (the seat
// that just took the crown), not the trigger's controller -- so the 2 life is
// taken from the opponent, and the gain goes to seat 0.
func TestKnightsOfTheBlackRoseDrainReadsTriggeredPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := linkBoard(t, reg, []string{"Knights of the Black Rose"}, nil)
	resolveKnightsETB(t, e)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})

	// Seat 1 (the opponent) takes the crown; the drain must hit seat 1.
	life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
	e.emit(events.Event{Kind: events.MonarchChange, Player: 1})
	requireOneEventTrigger(t, e, "Knights of the Black Rose")
	e.putTriggersOnStack()
	e.resolveTop()
	if e.G.Players[1].Life != life1-2 || e.G.Players[0].Life != life0+2 {
		t.Fatalf("drain went the wrong way: seat0 %d->%d, seat1 %d->%d (want seat0 +2, seat1 -2)",
			life0, e.G.Players[0].Life, life1, e.G.Players[1].Life)
	}
}
