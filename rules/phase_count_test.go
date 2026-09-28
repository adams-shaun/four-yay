package rules

// Forge's `PhaseCount$` narrows a `Phase$` gate to the Nth member of the
// named phase set in TURN ORDER. The corpus's 29 carriers all write
// `Phase$ Main | PhaseCount$ 2` -- "at the beginning of your second main
// phase" -- and before this gate a `Phase$ Main` trigger fired at BOTH main
// phases (an extra fire). The gate lives in the mode-shared phaseGate
// (rules/trigger_match.go), so every trigger mode that carries a Phase$ gate
// inherits it, and the Nth member is read from state.StepSet.Ordinal over the
// SAME parsed set ParsePhases produced -- one parser, so the two cannot
// disagree.
//
// The Lost Monarch of Ifnir fixture (Eternal Might, drc) is the brief's named
// card. Its real compiled trigger carries an intervening-if (`CheckSVar$ X`,
// "if a player was dealt combat damage by a Zombie this turn") whose count
// body -- `PlayerCountDefinedRegistered$HasPropertywasDealtCombatDamageThisTurnBy
// Zombie GE1` -- this build does not evaluate, so the trigger is suppressed
// at EVERY step by triggerMatches' CR 603.4 gate. That condition is orthogonal
// to the Phase/PhaseCount gate this ticket fixes: the first test measures the
// gate directly on the real compiled trigger, and the second drops only the
// unread condition param to show the whole trigger (Phase gate + ValidPlayer$
// + zone gate) matching at the second main phase and nowhere else. Ninja
// Pizza, a condition-free carrier of the identical `Phase$ Main |
// PhaseCount$ 2` shape, pins the same behaviour end to end through the
// ordinary queue path.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// stepOnce emits the StepChange the engine's own step advance would emit, so
// phaseGate reads the live step, and clears any queued triggers (they are
// irrelevant to the gate assertions below).
func stepOnce(e *Engine, s state.Step) {
	e.emit(events.Event{Kind: events.StepChange, Step: s})
	e.pendingTriggers = nil
}

// TestLostMonarchOfIfnirPhaseCountSecondMain pins the brief's card at the
// gate: the real compiled `Mode$ Phase | Phase$ Main | PhaseCount$ 2` trigger
// passes the Phase/PhaseCount gate at the second main phase only -- NOT the
// first, nor any other step.
func TestLostMonarchOfIfnirPhaseCountSecondMain(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Lost Monarch of Ifnir")
	tr := crTriggerFixture(t, e, id, "Phase", "Mill")
	if tr.Params["Phase"] != "Main" || tr.Params["PhaseCount"] != "2" {
		t.Fatalf("corpus fixture changed: Phase=%q PhaseCount=%q",
			tr.Params["Phase"], tr.Params["PhaseCount"])
	}
	for s := state.Step(0); s <= state.StepCleanup; s++ {
		stepOnce(e, s)
		if got, want := e.phaseGate(tr), s == state.StepMain2; got != want {
			t.Fatalf("step %s: PhaseCount gate matched=%v, want %v", s, got, want)
		}
	}
}

// TestLostMonarchOfIfnirTriggerMatchesSecondMainOnly drops ONLY the unread
// CheckSVar$X condition (a separate defect) from a copy of the real compiled
// trigger, so the full triggerMatches evaluation -- Phase gate, ValidPlayer$
// and the Battlefield zone gate together -- can be observed. It matches at
// the second main phase and nowhere else.
func TestLostMonarchOfIfnirTriggerMatchesSecondMainOnly(t *testing.T) {
	e, id := phaseCardEngine(t, "Lost Monarch of Ifnir")
	tr := crTriggerFixture(t, e, id, "Phase", "Mill")
	// Clone before deleting: tr.Params is the SHARED corpus registry card's
	// map, and deleting from it in place polluted every later test that read
	// Lost Monarch of Ifnir (its CheckSVar$ gate silently vanished).
	params := make(map[string]string, len(tr.Params))
	for k, v := range tr.Params {
		params[k] = v
	}
	tr.Params = params
	delete(tr.Params, "CheckSVar")
	for s := state.Step(0); s <= state.StepCleanup; s++ {
		stepOnce(e, s)
		got := e.triggerMatches(tr, id, events.Event{Kind: events.StepChange, Step: s}, nil)
		if want := s == state.StepMain2; got != want {
			t.Fatalf("step %s: triggerMatches=%v, want %v (the CheckSVar condition was removed)", s, got, want)
		}
	}
}

// TestNinjaPizzaPhaseCountQueuesOnlyAtSecondMain pins the full queue path on
// a condition-free real corpus carrier of the same shape: exactly one trigger
// queues at the second main phase and none at the first.
func TestNinjaPizzaPhaseCountQueuesOnlyAtSecondMain(t *testing.T) {
	t.Parallel()
	e, id := phaseCardEngine(t, "Ninja Pizza")
	tr := crTriggerFixture(t, e, id, "Phase", "Token")
	if tr.Params["Phase"] != "Main" || tr.Params["PhaseCount"] != "2" {
		t.Fatalf("corpus fixture changed: Phase=%q PhaseCount=%q",
			tr.Params["Phase"], tr.Params["PhaseCount"])
	}
	assertPhaseFires(t, e, id, state.StepMain2)
}

// TestPhaseCountAppliesToEveryModeWithAPhaseGate pins the class: PhaseCount$
// is enforced by the mode-shared phaseGate, so a NON-Phase mode carrying a
// Phase$ gate is narrowed identically. `PhaseCount$ 1` selects the first
// main phase only; a non-numeric or non-positive value fails closed at every
// step.
func TestPhaseCountAppliesToEveryModeWithAPhaseGate(t *testing.T) {
	t.Parallel()
	e, _ := phaseCardEngine(t, "Mountain")
	gate := func(count string) map[state.Step]bool {
		t.Helper()
		out := map[state.Step]bool{}
		for s := state.Step(0); s <= state.StepCleanup; s++ {
			stepOnce(e, s)
			tr := cards.Trigger{Mode: "SpellCast", Params: map[string]string{
				"Phase": "Main", "PhaseCount": count,
			}}
			if e.phaseGate(tr) {
				out[s] = true
			}
		}
		return out
	}
	first := gate("1")
	if !first[state.StepMain1] || first[state.StepMain2] {
		t.Fatalf("PhaseCount 1 should gate Main1 only, got %v", first)
	}
	second := gate("2")
	if second[state.StepMain1] || !second[state.StepMain2] {
		t.Fatalf("PhaseCount 2 should gate Main2 only, got %v", second)
	}
	for _, bad := range []string{"0", "-1", "x"} {
		if got := gate(bad); len(got) != 0 {
			t.Fatalf("PhaseCount %q must fail closed at every step, matched %v", bad, got)
		}
	}
	// A malformed PhaseCount paired with an unresolvable Phase$ must still
	// fail closed (the Phase$ half is checked first).
	if e.phaseGate(cards.Trigger{Mode: "SpellCast", Params: map[string]string{
		"Phase": "NotAPhase", "PhaseCount": "2",
	}}) {
		t.Fatal("unknown Phase$ with PhaseCount$ must fail closed")
	}
}
