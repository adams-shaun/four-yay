package rules

// Kernel-era restorations of the tests W3 removed from putcounter_optional_test.go: the
// same behaviour driven through the resolution kernel (the only ask path).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestTalusPaladinMayPutElectionPosesAndDeclinePlacesNothing: the trigger's
// Optional$ PutCounter poses the yes/no election; answering NO emits no
// CounterChange at all and the trigger completes (no pending decision).
func TestTalusPaladinMayPutElectionPosesAndDeclinePlacesNothingKernel(t *testing.T) {
	t.Parallel()
	e, cfg, pal := kr7TalusPaladinGame(t, 911)
	answerPutOptional(t, e, 1) // no
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the decline: %+v", d)
	}
	if evs := counterChangeEventsFor(e, pal); len(evs) != 0 {
		t.Fatalf("decline emitted %d CounterChange events: %+v", len(evs), evs)
	}
	if got := putCounterCountersOf(t, e, pal); got != 0 {
		t.Fatalf("paladin P1P1 counters = %d, want 0 after the decline", got)
	}
	replayCheck(t, e, cfg)
}

// TestTalusPaladinMayPutAcceptPlacesExactlyOneCounter: answering YES places
// exactly the one P1P1 counter (byte-identical to the pre-ask silent put).
func TestTalusPaladinMayPutAcceptPlacesExactlyOneCounterKernel(t *testing.T) {
	t.Parallel()
	e, cfg, pal := kr7TalusPaladinGame(t, 912)
	answerPutOptional(t, e, 0) // yes
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the accept: %+v", d)
	}
	evs := counterChangeEventsFor(e, pal)
	if len(evs) != 1 || evs[0].Counter != "P1P1" || evs[0].Amount != 1 {
		t.Fatalf("accept emitted %+v, want exactly one P1P1 of amount 1", evs)
	}
	if got := putCounterCountersOf(t, e, pal); got != 1 {
		t.Fatalf("paladin P1P1 counters = %d, want 1 after the accept", got)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterOptionalNonBattlefieldRecipientElection: with the recipient
// off the battlefield (the trigger's source still in hand) the put WOULD
// place something -- CR 122.1 lets counters live on an object in any zone,
// so a hand card is a live recipient -- and the election is posed (the
// round-1 CR 122.1 fix made putCounterWouldPlace match the placement; the
// old pin here asserted the battlefield-only premise and the election was
// "never posed", which is what made the module gate red in round 3). This
// is the rules-side end-to-end twin of the effects-package pins: the
// decline places nothing and the trigger completes; the accept places one
// P1P1 counter on the hand object through the real trigger resolution path.
func TestPutCounterOptionalNonBattlefieldRecipientElectionKernel(t *testing.T) {
	t.Parallel()
	// Decline: the election is posed with the recipient in hand; answering no
	// places nothing and the resolution completes.
	e, cfg, pal := gateFixture(t, 913, "Talus Paladin")
	if o := e.G.Obj(pal); o == nil {
		t.Fatalf("precondition: trigger source %d is not a live object", pal)
	} else if o.Zone == state.ZBattlefield {
		t.Fatalf("precondition: recipient is on the battlefield, want a non-battlefield zone (%v)", o.Zone)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: pal, Player: 0, Amount: 0})
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.ResumeKind != "put_optional" {
		t.Fatalf("pending = %+v, want the put_optional election (the hand recipient is a live CR 122.1 recipient)", d)
	}
	answerPutOptional(t, e, 1) // no
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the decline: %+v", d)
	}
	if evs := counterChangeEventsFor(e, pal); len(evs) != 0 {
		t.Fatalf("decline emitted %d CounterChange events: %+v", len(evs), evs)
	}
	if got := putCounterCountersOf(t, e, pal); got != 0 {
		t.Fatalf("paladin P1P1 counters = %d, want 0 after the decline", got)
	}
	replayCheck(t, e, cfg)

	// Accept: exactly one P1P1 counter is placed on the hand object (CR 122.1:
	// counters live on objects in any zone, and events.Apply folds the
	// CounterChange for any live object regardless of zone).
	e2, cfg2, pal2 := gateFixture(t, 919, "Talus Paladin")
	if o := e2.G.Obj(pal2); o == nil || o.Zone == state.ZBattlefield {
		t.Fatalf("precondition: recipient %d must be a live non-battlefield object, got zone %v", pal2, o)
	}
	e2.emit(events.Event{Kind: events.TriggerPush, Obj: pal2, Player: 0, Amount: 0})
	e2.pending = nil // resolve directly, outside the pending priority window
	e2.resolveTop()
	answerPutOptional(t, e2, 0) // yes
	if d := e2.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the accept: %+v", d)
	}
	evs := counterChangeEventsFor(e2, pal2)
	if len(evs) != 1 || evs[0].Counter != "P1P1" || evs[0].Amount != 1 {
		t.Fatalf("accept emitted %+v, want exactly one P1P1 of amount 1 on the hand object", evs)
	}
	if got := putCounterCountersOf(t, e2, pal2); got != 1 {
		t.Fatalf("paladin P1P1 counters = %d, want 1 after the accept", got)
	}
	replayCheck(t, e2, cfg2)
}

// TestBlackWidowMayPutDeclinePlacesNoCounterAndRunsTheChain: the "If you
// don't, ..." card's Optional$ PutCounter sits UNDER its DigUntil sub-chain;
// driving the real trigger end to end, the decline places no P1P1 counter,
// emits no Note, and the trigger completes -- the chained DBEffect runs on
// the decline exactly as the oracle says (its Remembered-empty EQ0 gate is
// the decline's grant; the ACCEPT path's grant gating is RememberPut$, out
// of scope here).
func TestBlackWidowMayPutDeclinePlacesNoCounterAndRunsTheChainKernel(t *testing.T) {
	t.Parallel()
	// Eight extras so the top-7 opening deal leaves a nonland in the library
	// for the DigUntil to find (a mountain-only library would exhaust the
	// scan before the put's election was ever reached).
	e, cfg, widow := gateFixture(t, 914, "Black Widow, Super Spy",
		gearTrinketSrc, gearTrinketSrc, gearTrinketSrc, gearTrinketSrc,
		gearTrinketSrc, gearTrinketSrc, gearTrinketSrc, gearTrinketSrc)
	widow = gateMoveFromLibrary(t, e, "Black Widow, Super Spy", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TriggerPush, Obj: widow, Player: 0, Amount: 0})
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	answerPutOptional(t, e, 1) // no
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the decline: %+v", d)
	}
	if evs := counterChangeEventsFor(e, widow); len(evs) != 0 {
		t.Fatalf("decline emitted %d CounterChange events: %+v", len(evs), evs)
	}
	if got := putCounterCountersOf(t, e, widow); got != 0 {
		t.Fatalf("widow P1P1 counters = %d, want 0 after the decline", got)
	}
	replayCheck(t, e, cfg)
}

// TestSynthEradicatorMayPutEnergyDeclinePlacesNoneAndAcceptPlacesTwo pins
// the PLAYER-recipient shape (Defined$ You, CounterType$ ENERGY,
// CounterNum$ 2): the decline places no energy counter, the accept places
// exactly the one batch of 2.
func TestSynthEradicatorMayPutEnergyDeclinePlacesNoneAndAcceptPlacesTwoKernel(t *testing.T) {
	t.Parallel()
	e, cfg, synth := gateFixture(t, 915, "Synth Eradicator")
	synth = gateMoveFromLibrary(t, e, "Synth Eradicator", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TriggerPush, Obj: synth, Player: 0, Amount: 0})
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	answerPutOptional(t, e, 1) // no
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the decline: %+v", d)
	}
	if evs := playerCounterEventsFor(e, 0, "ENERGY"); len(evs) != 0 {
		t.Fatalf("decline emitted %d energy events: %+v", len(evs), evs)
	}
	replayCheck(t, e, cfg)

	e2, cfg2, synth2 := gateFixture(t, 916, "Synth Eradicator")
	synth2 = gateMoveFromLibrary(t, e2, "Synth Eradicator", state.ZBattlefield)
	e2.emit(events.Event{Kind: events.TriggerPush, Obj: synth2, Player: 0, Amount: 0})
	e2.pending = nil // resolve directly, outside the pending priority window
	e2.resolveTop()
	answerPutOptional(t, e2, 0) // yes
	if d := e2.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("a non-priority decision is still pending after the accept: %+v", d)
	}
	evs := playerCounterEventsFor(e2, 0, "ENERGY")
	if len(evs) != 1 || evs[0].Amount != 2 {
		t.Fatalf("accept emitted %+v, want exactly one ENERGY batch of 2", evs)
	}
	replayCheck(t, e2, cfg2)
}

// kr7TalusPaladinGame is talusPaladinGame resolving the pushed trigger with
// no priority decision pending (the kernel serves no ask behind one).
func kr7TalusPaladinGame(t *testing.T, seed uint64) (*Engine /*cfg*/, Config, state.ObjID) {
	t.Helper()
	e, cfg, pal := gateFixture(t, seed, "Talus Paladin")
	pal = gateMoveFromLibrary(t, e, "Talus Paladin", state.ZBattlefield)
	e.emit(events.Event{Kind: events.TriggerPush, Obj: pal, Player: 0, Amount: 0})
	e.pending = nil // resolve directly, outside the pending priority window
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "put_optional" {
		t.Fatalf("pending = %+v, want the may-put election", d)
	}
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want 0 (the controller)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("options = %+v, want yes then no (option 0 = yes)", d.Options)
	}
	if d.Min != 1 || d.Max != 1 {
		t.Fatalf("bounds %d..%d, want 1..1", d.Min, d.Max)
	}
	return e, cfg, pal
}
