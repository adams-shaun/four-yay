package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A Phase delayed registration whose IsPresent$ names Card.IsTriggerRemembered
// only fires at the phase when one of the registration's own Remembered cards
// is in the PresentZone$ it names (Bank Job: "at the beginning of the next end
// step, if that card is still exiled"). The condition rides the registration's
// Text as |IP= / |PZ= suffixes, is folded back into state.DelayedTrigger by
// events.Apply's DelayedRegister case, and is evaluated by
// rules/trigger_delayed.go checkDelayedTriggers at the phase occurrence with
// the registration's capture bound as the IsTriggerRemembered referent.
//
// These tests drive the real producer (effects.Resolve on a DB$ DelayedTrigger
// SA, not a hand-built DelayedRegister event), so a regression that drops the
// serialization in effects/misc.go effDelayedTrigger is caught as well as one
// that drops the fire-time gate.

// delayedPresentSource is the card whose SVar Trig the delayed trigger's
// Execute$ names: its resolution gains 2 life, an observable stand-in for a
// token-creating/exiling body without importing the GPL Forge corpus.
const delayedPresentSource = "Name:Present promise\nTypes:Creature\nPT:2/2\nSVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"

func delayedPresentSA() *cards.SA {
	return &cards.SA{Kind: "DB", API: "DelayedTrigger", Params: map[string]string{
		"Mode":        "Phase",
		"Phase":       "End of Turn",
		"Execute":     "Trig",
		"IsPresent":   "Card.IsTriggerRemembered",
		"PresentZone": "Exile",
	}}
}

// registerDelayedPresent resolves a DB$ DelayedTrigger with captured as the
// resolving chain's Remembered, the way the ChangeZone/Effect chain that
// precedes Bank Job's registration supplies it.
func registerDelayedPresent(t *testing.T, e *Engine, src, captured state.ObjID) {
	t.Helper()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: captured}}}, delayedPresentSA())
}

func TestDelayedPhasePresentGateFiresWhenRememberedInZone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, delayedPresentSource)
	captured := inZoneCard(t, e, 1, state.ZExile, "Name:Exiled card\nTypes:Sorcery\nOracle:x\n")
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(captured).Zone != state.ZExile {
		t.Fatal("precondition: source on battlefield and captured card in exile required")
	}

	registerDelayedPresent(t, e, src, captured)

	// Fold assertion: the real producer's Text suffix was decoded back into the
	// registration, so the condition survives replay reconstruction.
	if len(e.G.Delayed) != 1 {
		t.Fatalf("precondition: registrations = %d, want 1", len(e.G.Delayed))
	}
	dt := e.G.Delayed[0]
	if dt.PresentSpec != "Card.IsTriggerRemembered" || dt.PresentZone != "Exile" {
		t.Fatalf("precondition: folded condition = IP=%q PZ=%q, want Card.IsTriggerRemembered / Exile", dt.PresentSpec, dt.PresentZone)
	}
	if len(dt.Remembered) != 1 || dt.Remembered[0].Obj != captured {
		t.Fatalf("precondition: capture = %+v, want captured %d", dt.Remembered, captured)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("remembered card in exile: pending=%d, want 1", len(e.pendingTriggers))
	}
	life := e.G.Players[0].Life
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("no delayed execute on stack")
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life+2 {
		t.Errorf("execute life=%d, want %d", got, life+2)
	}
}

func TestDelayedPhasePresentGateSkipsWhenRememberedOutOfZone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, delayedPresentSource)
	// The remembered card is NOT in Exile; the condition must hold it back.
	captured := onBoard(t, e, 1, "Name:Not exiled\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(captured).Zone == state.ZExile {
		t.Fatal("precondition: captured card must not start in exile")
	}

	registerDelayedPresent(t, e, src, captured)

	// Precondition: the handler DID run -- the condition folded and the
	// one-shot registration is live. A green "nothing fires" below cannot be
	// the whole feature being unregistered.
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].PresentSpec != "Card.IsTriggerRemembered" {
		t.Fatalf("precondition: folded condition = %+v", e.G.Delayed)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("card absent from exile fired: pending=%d", len(e.pendingTriggers))
	}
	if len(e.G.Delayed) != 1 {
		t.Fatalf("one-shot registration consumed by a failed gate: delayed=%d", len(e.G.Delayed))
	}
	life := e.G.Players[0].Life
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %d, want 0", len(e.G.Stack))
	}

	// The gate is the only blocker: move the SAME remembered card into Exile
	// and the registration fires at the next occurrence of the phase.
	e.emit(events.Event{Kind: events.MoveZone, Obj: captured, From: state.ZBattlefield, To: state.ZExile})
	if e.G.Obj(captured).Zone != state.ZExile {
		t.Fatal("precondition: captured card was not moved to exile")
	}
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("remembered card now in exile: pending=%d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if got := e.G.Players[0].Life; got != life+2 {
		t.Errorf("execute life=%d, want %d", got, life+2)
	}
}

// TestDelayedPhasePresentGateWithValidPlayer pins the real corpus shape:
// Bank Job and Grinning Totem carry ValidPlayer$ You TOGETHER with IsPresent$
// Card.IsTriggerRemembered | PresentZone$ Exile. The two suffixes must both
// survive the decode -- the player gate is not allowed to swallow the presence
// condition that follows it in the Text.
func TestDelayedPhasePresentGateWithValidPlayer(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, delayedPresentSource)
	captured := inZoneCard(t, e, 1, state.ZExile, "Name:Exiled card\nTypes:Sorcery\nOracle:x\n")
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(captured).Zone != state.ZExile || e.G.Active != 0 {
		t.Fatal("precondition: source on battlefield, captured in exile, active seat 0")
	}

	sa := delayedPresentSA()
	sa.Params["ValidPlayer"] = "You"
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: captured}}}, sa)

	if len(e.G.Delayed) != 1 {
		t.Fatalf("precondition: registrations = %d, want 1", len(e.G.Delayed))
	}
	dt := e.G.Delayed[0]
	if dt.ValidPlayer != "You" || dt.PresentSpec != "Card.IsTriggerRemembered" || dt.PresentZone != "Exile" {
		t.Fatalf("folded VP/IP/PZ = %q / %q / %q, want You / Card.IsTriggerRemembered / Exile", dt.ValidPlayer, dt.PresentSpec, dt.PresentZone)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("VP+present registration with card in exile: pending=%d, want 1", len(e.pendingTriggers))
	}
}

// TestDelayedPhasePresentGateLegacyRegistrationUngated proves backward
// compatibility: a registration logged WITHOUT the condition suffixes (every
// pre-existing registration) decodes with an empty PresentSpec and fires
// ungated, exactly as before.
func TestDelayedPhasePresentGateLegacyRegistrationUngated(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, delayedPresentSource)
	captured := inZoneCard(t, e, 1, state.ZExile, "Name:Exiled card\nTypes:Sorcery\nOracle:x\n")
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(captured).Zone != state.ZExile {
		t.Fatal("precondition: source on battlefield and captured card in exile required")
	}

	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: state.StepEnd,
		Counter: "Trig", IDs: []state.ObjID{captured}, Text: "End of Turn"})
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].PresentSpec != "" || e.G.Delayed[0].PresentZone != "" || e.G.Delayed[0].PresentCompare != "" {
		t.Fatalf("legacy decode = %+v, want empty condition fields", e.G.Delayed)
	}
	if len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != captured {
		t.Fatalf("legacy decode lost the capture: %+v", e.G.Delayed[0].Remembered)
	}

	e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("legacy ungated registration pending=%d, want 1", len(e.pendingTriggers))
	}
}
