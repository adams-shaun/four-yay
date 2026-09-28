package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 702.25: Phasing (702.25a–f); 702.25b/d phased-out status.
// CR 702.26: Phasing (keyword), 702.26a phase-out at the controller's untap
// step.
// CR 502.4: the untap step's turn-based action phases permanents in.
// These are engine-boundary probes on real corpus cards, never fabricated
// rules text. Every leaf passes with the fix, so all run in the ordinary
// lane; `make conformance` runs them via -run TestCR.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cr702corpusCard looks a real corpus card up by name.
func cr702corpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	return tokenReplCorpusCard(t, name)
}

// TestCR702PhasedOutPermanentIsNotATarget pins CR 702.25b: a phased-out
// permanent is treated as though it does not exist, so the target census
// never offers it. The probe reads the same candidatesFor the target ask
// uses; a phased-in control creature IS offered, so the exclusion is real,
// not a vacuous empty list.
func TestCR702PhasedOutPermanentIsNotATarget(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 401, "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, cr702corpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	sa := &cards.SA{Params: map[string]string{"ValidTgts": "Creature"}}
	// Precondition: while phased in, the creature IS offered (the census has
	// a real candidate to lose).
	if got := e.legalTargetCandidates(0, 0, 0, sa); !cr702hasObj(got, bears) {
		t.Fatal("precondition: a phased-in creature is not offered as a Creature target")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut {
		t.Fatal("precondition: Bears are not phased out")
	}
	if got := e.legalTargetCandidates(0, 0, 0, sa); cr702hasObj(got, bears) {
		t.Fatal("CR 702.25b: a phased-out permanent was offered as a target")
	}
	replayCheck(t, e, cfg)
}

// TestCR702PhasedOutPermanentStaticAbilityIsOff pins CR 702.25b: a phased-out
// permanent's static abilities are off, so a lord's pump does not reach it.
// Glorious Anthem is the lord; the Bears' effective power is the probe.
func TestCR702PhasedOutPermanentStaticAbilityIsOff(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 402, "Glorious Anthem", "Grizzly Bears")
	anthem := moveSeededCard(t, e, 0, cr702corpusCard(t, "Glorious Anthem"), state.ZBattlefield)
	bears := moveSeededCard(t, e, 0, cr702corpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	if o := e.G.Obj(anthem); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: Glorious Anthem is not on the battlefield")
	}
	// Precondition: the pump applies while the creature is phased in.
	if got := e.Power(bears); got != 3 {
		t.Fatalf("precondition: Grizzly Bears power = %d, want 3 (2/2 + Glorious Anthem)", got)
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	if got := e.Power(bears); got != 2 {
		t.Fatalf("CR 702.25b: phased-out Bears power = %d, want 2 (the lord's static must be off)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCR702PhasedOutPermanentDoesNotAttack pins CR 702.25b: a phased-out
// permanent does not participate in combat. canAttack is the one gate both
// the attacker offer and the validator read. Raging Goblin carries Haste, so
// the only gate under test is phasing (no summoning-sickness setup needed).
func TestCR702PhasedOutPermanentDoesNotAttack(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 403, "Raging Goblin")
	goblin := moveSeededCard(t, e, 0, cr702corpusCard(t, "Raging Goblin"), state.ZBattlefield)
	if o := e.G.Obj(goblin); o == nil || o.Zone != state.ZBattlefield || !e.HasKeyword(goblin, "Haste") {
		t.Fatal("precondition: Raging Goblin is not a hasty battlefield creature")
	}
	if !e.canAttack(goblin) {
		t.Fatal("precondition: a phased-in, hasty creature cannot attack")
	}
	e.emit(events.Event{Kind: events.PhaseOut, Obj: goblin, Amount: 1})
	if e.canAttack(goblin) {
		t.Fatal("CR 702.25b: a phased-out creature was allowed to attack")
	}
	replayCheck(t, e, cfg)
}

// TestCR702PhasedOutPermanentPhasesInAtUntap pins CR 702.25d with CR 502.4:
// a phased-out permanent phases in at its controller's untap step.
func TestCR702PhasedOutPermanentPhasesInAtUntap(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 404, "Grizzly Bears")
	bears := moveSeededCard(t, e, 0, cr702corpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	e.emit(events.Event{Kind: events.PhaseOut, Obj: bears, Amount: 1})
	if o := e.G.Obj(bears); o == nil || !o.PhasedOut {
		t.Fatal("precondition: Bears are not phased out")
	}
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	if o := e.G.Obj(bears); o == nil || o.PhasedOut {
		t.Fatal("CR 702.25d: a phased-out permanent did not phase in at its controller's untap step")
	}
	replayCheck(t, e, cfg)
}

// TestCR702PhasingKeywordTogglesAtUntapStep pins CR 702.26a: a permanent
// with the Phasing keyword phases OUT at the beginning of its controller's
// untap step (before that step's untap action), and phases back IN at the
// next such step (the CR 702.25d scan serves the phase-in half of the
// toggle) — one toggle per step, so the phase-in is not immediately undone.
func TestCR702PhasingKeywordTogglesAtUntapStep(t *testing.T) {
	t.Parallel()
	e, cfg := phasesGame(t, 405, "Katabatic Winds", "Grizzly Bears")
	kw := moveSeededCard(t, e, 0, cr702corpusCard(t, "Katabatic Winds"), state.ZBattlefield)
	bears := moveSeededCard(t, e, 0, cr702corpusCard(t, "Grizzly Bears"), state.ZBattlefield)
	// Precondition: the carrier is really a phased-in battlefield permanent
	// carrying the keyword; the step under test is its controller's untap
	// step; and the Bears are tapped so the step's untap action emits a real
	// Untap event the phase-out must precede.
	if o := e.G.Obj(kw); o == nil || o.Zone != state.ZBattlefield || o.PhasedOut {
		t.Fatalf("precondition: Katabatic Winds not a phased-in battlefield permanent: %+v", o)
	}
	if !e.HasKeyword(kw, "Phasing") {
		t.Fatal("precondition: Katabatic Winds does not carry the Phasing keyword")
	}
	e.emit(events.Event{Kind: events.Tap, Obj: bears})
	if o := e.G.Obj(bears); o == nil || !o.Tapped {
		t.Fatal("precondition: Bears not a tapped battlefield permanent")
	}
	startTurn := e.G.Turn

	driveToStepAll(t, e, startTurn+2, 0, state.StepUpkeep)

	// CR 702.26a: the keyword phased it out before the step's untap action;
	// the PhaseOut event precedes the step's own Untap event for the Bears.
	outIdx, untapIdx := -1, -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.PhaseOut && ev.Obj == kw && ev.Amount >= 1 && outIdx < 0 {
			outIdx = i
		}
		if ev.Kind == events.Untap && ev.Obj == bears && untapIdx < 0 {
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
		t.Fatalf("CR 702.26a: the keyword phase-out (event %d) must precede the step's Untap action (event %d)", outIdx, untapIdx)
	}
	if o := e.G.Obj(kw); o == nil || !o.PhasedOut || o.WontPhaseInNormal {
		t.Fatalf("CR 702.26a: Katabatic Winds PhasedOut=%v WontPhaseInNormal=%v, want true/false", o != nil && o.PhasedOut, o != nil && o.WontPhaseInNormal)
	}

	// The toggle's return half: at the next untap step it phases IN before
	// the untap action, and the keyword does not re-phase it out that step.
	driveToStepAll(t, e, e.G.Turn+2, 0, state.StepUpkeep)
	if o := e.G.Obj(kw); o == nil || o.PhasedOut {
		t.Fatalf("CR 702.26a/702.25d: Katabatic Winds PhasedOut=%v, want false after the next untap step", o != nil && o.PhasedOut)
	}
	out2, in2 := 0, 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.PhaseOut || ev.Obj != kw {
			continue
		}
		if ev.Amount >= 1 {
			out2++
		} else {
			in2++
		}
	}
	if out2 != 1 || in2 != 1 {
		t.Fatalf("CR 702.26a: want exactly one phase-out and one phase-in across the two untap steps, got %d/%d", out2, in2)
	}

	replayCheck(t, e, cfg)
}

// cr702hasObj reports whether any candidate names the object.
func cr702hasObj(cands []targetCandidate, id state.ObjID) bool {
	for _, c := range cands {
		if c.obj == id {
			return true
		}
	}
	return false
}
