package rules

// CR 702.26 — Phasing (keyword). A permanent with the printed Phasing
// keyword "phases in or out before [the active player] untaps during each of
// [his or her] untap steps" (CR 702.26a). This is the filing card end-to-end
// regression on Katabatic Winds: the keyword alone (no api:Phases body)
// phases the permanent out at the beginning of its controller's untap step,
// and it phases back in at the next such step (CR 702.25d, which the same
// untap-step scan serves). References:
// - Magic: The Gathering Comprehensive Rules, 2026-08-07 (CR 702.26a).
// - rules/turn.go finishUntapStep (the untap-step turn-based action scan).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestKatabaticWindsKeywordPhasesOutAndIn drives a real corpus carrier,
// Katabatic Winds (K:Phasing), through its controller's untap boundaries:
// phased in at first, it phases OUT at seat 0's next untap step before the
// step's untap action, then phases back IN at the following one.
func TestKatabaticWindsKeywordPhasesOutAndIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 407, "Katabatic Winds")
	kw := moveSeededCard(t, e, 0, cr702corpusCard(t, "Katabatic Winds"), state.ZBattlefield)

	// Precondition: the carrier is really a phased-in battlefield permanent
	// whose keyword is derived, and the turn boundary under test is its own
	// controller's (seat 0's) next untap step.
	if o := e.G.Obj(kw); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Katabatic Winds not a phased-in battlefield permanent: %+v", o)
	}
	if !e.HasKeyword(kw, "Phasing") {
		t.Fatal("precondition: Katabatic Winds does not carry the Phasing keyword")
	}
	if e.G.Active != 0 {
		t.Fatalf("precondition: active player is %d, want 0", e.G.Active)
	}
	startTurn := e.G.Turn

	// Drive to seat 0's NEXT untap step (two turns later in a 2-seat game),
	// running through the step's turn-based action: the drive target is
	// upkeep, so the untap step's scan has fully run by the time we stop.
	driveToStepAll(t, e, startTurn+2, 0, state.StepUpkeep)

	// CR 702.26a: the keyword phased it out, and CR 702.25d's normal
	// phase-in is what will return it, so the phase-out must NOT carry the
	// wont-phase-in-normal marker.
	o := e.G.Obj(kw)
	if o == nil || !o.PhasedOut {
		t.Fatalf("CR 702.26a: Katabatic Winds PhasedOut=%v, want true at its controller's untap step", o != nil && o.PhasedOut)
	}
	if o.WontPhaseInNormal {
		t.Fatal("CR 702.26a: the keyword's phase-out must not set WontPhaseInNormal (it toggles back in next step)")
	}
	phaseOutKw := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.PhaseOut && ev.Obj == kw && ev.Amount >= 1 {
			phaseOutKw++
		}
	}
	if phaseOutKw != 1 {
		t.Fatalf("CR 702.26a: expected exactly one PhaseOut(Amount 1) event for Katabatic Winds, got %d", phaseOutKw)
	}

	// Drive to the following untap step: the carrier phases back IN before
	// the step's untap action.
	nextTurn := e.G.Turn + 2
	driveToStepAll(t, e, nextTurn, 0, state.StepUpkeep)
	if o := e.G.Obj(kw); o == nil || o.PhasedOut {
		t.Fatalf("CR 702.26a: Katabatic Winds PhasedOut=%v, want false after its controller's next untap step", o != nil && o.PhasedOut)
	}
	phaseInKw := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.PhaseOut && ev.Obj == kw && ev.Amount < 0 {
			phaseInKw++
		}
	}
	if phaseInKw != 1 {
		t.Fatalf("CR 702.26a: expected exactly one phase-in event for Katabatic Winds, got %d", phaseInKw)
	}

	replayCheck(t, e, cfg)
}

// TestKatabaticWindsPhasesOutBeforeUntapAction pins the "before you untap"
// ordering of CR 702.26a: the keyword's phase-out event must precede the
// untap step's own Untap event for a tapped permanent, so the phased-out
// status is set before the action that untaps everything.
func TestKatabaticWindsPhasesOutBeforeUntapAction(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 408, "Katabatic Winds", "Grizzly Bears")
	kw := moveSeededCard(t, e, 0, cr702corpusCard(t, "Katabatic Winds"), state.ZBattlefield)
	bears := moveSeededCard(t, e, 0, cr702corpusCard(t, "Grizzly Bears"), state.ZBattlefield)

	// Precondition: the Bears are tapped on the battlefield, so this step's
	// untap action really emits an Untap event to order against.
	e.emit(events.Event{Kind: events.Tap, Obj: bears})
	if o := e.G.Obj(bears); o == nil || !o.Tapped {
		t.Fatal("precondition: Bears not tapped on the battlefield")
	}
	if o := e.G.Obj(kw); o == nil || o.PhasedOut {
		t.Fatal("precondition: Katabatic Winds not phased in on the battlefield")
	}
	startTurn := e.G.Turn

	driveToStepAll(t, e, startTurn+2, 0, state.StepUpkeep)

	// Both events must exist, and the phase-out must come first.
	outIdx, untapIdx := -1, -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.PhaseOut && ev.Obj == kw && ev.Amount >= 1 {
			outIdx = i
		}
		if ev.Kind == events.Untap && ev.Obj == bears {
			untapIdx = i
		}
	}
	if outIdx < 0 {
		t.Fatal("CR 702.26a: no keyword phase-out event was recorded")
	}
	if untapIdx < 0 {
		t.Fatal("precondition: no Untap event for the tapped Bears was recorded")
	}
	if outIdx > untapIdx {
		t.Fatalf("CR 702.26a: the keyword phase-out (index %d) must precede the step's Untap action (index %d)", outIdx, untapIdx)
	}
	if o := e.G.Obj(kw); o == nil || !o.PhasedOut {
		t.Fatalf("CR 702.26a: Katabatic Winds PhasedOut=%v, want true", o != nil && o.PhasedOut)
	}
	// The untap action still ran for the (unaffected) tapped Bears.
	if o := e.G.Obj(bears); o == nil || o.Tapped {
		t.Fatal("the step's untap action did not untap the (unaffected) tapped Bears")
	}

	replayCheck(t, e, cfg)
}

// TestPhasingKeywordOnNonactiveControllersUntapStepDoesNothing pins that the
// keyword fires only during its controller's untap step: during the OTHER
// player's untap step nothing happens to the carrier.
func TestPhasingKeywordOnNonactiveControllersUntapStepDoesNothing(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 409, "Katabatic Winds")
	kw := moveSeededCard(t, e, 0, cr702corpusCard(t, "Katabatic Winds"), state.ZBattlefield)
	if o := e.G.Obj(kw); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Katabatic Winds not a phased-in battlefield permanent: %+v", o)
	}
	startTurn := e.G.Turn

	// Drive to seat 1's next upkeep: the carrier's controller (seat 0) was
	// NOT active, so its keyword must not have fired.
	driveToStepAll(t, e, startTurn+1, 1, state.StepUpkeep)
	if o := e.G.Obj(kw); o == nil || o.PhasedOut {
		t.Fatalf("CR 702.26a: the keyword fired outside its controller's untap step: PhasedOut=%v", o != nil && o.PhasedOut)
	}

	replayCheck(t, e, cfg)
}

// TestPhasingKeywordDoesNotSuppressApiPhasesPhaseIn keeps the api:Phases
// side intact: a phased-out carrier (phased out by a real api:Phases body,
// WontPhaseInNormal untouched) still phases in at its controller's untap
// step, and the keyword does not immediately phase it back out the same
// step (one toggle per step).
func TestPhasingKeywordDoesNotSuppressApiPhasesPhaseIn(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 410, "Katabatic Winds")
	kw := moveSeededCard(t, e, 0, cr702corpusCard(t, "Katabatic Winds"), state.ZBattlefield)
	if !e.HasKeyword(kw, "Phasing") {
		t.Fatal("precondition: Katabatic Winds does not carry the Phasing keyword")
	}
	// Phase it out through the event path the api:Phases body uses (no
	// WontPhaseInNormal marker, so the normal phase-in reaches it).
	e.emit(events.Event{Kind: events.PhaseOut, Obj: kw, Amount: 1})
	if o := e.G.Obj(kw); o == nil || !o.PhasedOut || o.WontPhaseInNormal {
		t.Fatalf("precondition: Katabatic Winds not keyword-neutrally phased out: %+v", o)
	}
	startTurn := e.G.Turn

	driveToStepAll(t, e, startTurn+2, 0, state.StepUpkeep)

	// It phased in, and the keyword did NOT phase it back out this step.
	if o := e.G.Obj(kw); o == nil || o.PhasedOut {
		t.Fatalf("CR 702.25d: the phased-out carrier did not phase in at its controller's untap step: PhasedOut=%v", o != nil && o.PhasedOut)
	}
	phaseOuts := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.PhaseOut && ev.Obj == kw && ev.Amount >= 1 {
			phaseOuts++
		}
	}
	if phaseOuts != 1 {
		t.Fatalf("expected exactly one phase-out (the manual one) and no same-step keyword re-phase-out, got %d", phaseOuts)
	}

	replayCheck(t, e, cfg)
}
