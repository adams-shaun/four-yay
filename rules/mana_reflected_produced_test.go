// Tests for the ReflectProperty$ Produced family ("add one mana of any type
// that land produced"): the triggered mana ability must read the mana types
// the triggering tap ACTUALLY produced. The trigger is matched and queued in
// emitManaTap, before the mana effect runs, so its context's TriggerMana was
// historically empty and ManaReflected resolved no candidates and emitted a
// "found no mana to reflect" Note. rules/mana_activation.go's
// stampTriggeredManaProduced now binds the activation's ManaAdd batch to the
// trigger batch at CR 605.3b resolution. Kept in its own file so the ticket
// cannot conflict on a shared test file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kinnanTriggerScript is Kinnan, Bonder Prodigy's real triggered-mana shape
// (a nonland-permanent TapsForMana trigger), reduced to the parts the reflect
// reads. ValidCard$ Permanent.nonLand is the nonland filter the brief's
// "Kinnan + Forest = {G} only" case pins.
const kinnanTriggerScript = "Name:Kinnan, Bonder Prodigy\nManaCost:G U\nTypes:Legendary Creature Human Druid\nPT:2/2\n" +
	"T:Mode$ TapsForMana | ValidCard$ Permanent.nonLand | Activator$ You | Execute$ TrigMana | TriggerZones$ Battlefield | Static$ True | TriggerDescription$ Whenever you tap a nonland permanent for mana, add one mana of any type that permanent produced.\n" +
	"SVar:TrigMana:DB$ ManaReflected | ColorOrType$ Type | ReflectProperty$ Produced | Defined$ You\n" +
	"Oracle:x\n"

// manaRockScript is a {T}: Add {C} artifact -- the nonland permanent Kinnan
// reflects.
const manaRockScript = "Name:Mana Rock\nTypes:Artifact\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"

// reflectedSpan is the event log a single activation appended, with the
// ManaReflected-failure Note called out. The Note is the observable symptom
// the census recorded (unhandled_note): it must never appear once the
// produced set is bound.
type reflectedSpan struct {
	notes           []string
	manaReflectNote bool
}

func spanManaReflected(e *Engine, start int) reflectedSpan {
	var s reflectedSpan
	for _, ev := range e.L.Events[start:] {
		if ev.Kind != events.Note {
			continue
		}
		s.notes = append(s.notes, ev.Text)
		if ev.Text == "ManaReflected found no mana to reflect" {
			s.manaReflectNote = true
		}
	}
	return s
}

// TestTriggeredManaReflectedProduced drives the real priority activation
// path for the Produced reflect family. Each case asserts the precondition
// (the trigger permanent and the tapped permanent are on the battlefield),
// the pool the activation must leave, and that no ManaReflected-failure Note
// was emitted -- so reverting the stamping hunk turns the pool assertion red
// rather than silently passing.
func TestTriggeredManaReflectedProduced(t *testing.T) {
	t.Run("ManaFlareSeat0Forest", func(t *testing.T) {
		e := handEngine(t)
		flare := onBoard(t, e, 0, manaFlareScript)
		forest := onBoard(t, e, 0, forestScript())
		if e.G.Obj(flare).Zone != state.ZBattlefield || e.G.Obj(forest).Zone != state.ZBattlefield {
			t.Fatalf("precondition: Mana Flare/Forest not on the battlefield (%v/%v)",
				e.G.Obj(flare).Zone, e.G.Obj(forest).Zone)
		}
		e.priorityRound()
		start := len(e.L.Events)
		activateMana(t, e, forest)
		if got := e.G.Players[0].Pool[state.MG]; got != 2 {
			t.Fatalf("Mana Flare + Forest green pool = %d, want 2 (Forest's {G} + the reflected {G})", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 2 {
			t.Fatalf("total pool = %d, want 2", got)
		}
		if s := spanManaReflected(e, start); s.manaReflectNote {
			t.Fatalf("Mana Flare emitted the failure Note: notes=%q", s.notes)
		}
	})

	t.Run("ManaFlareSeat1ForestIsSymmetric", func(t *testing.T) {
		e := handEngine(t)
		flare := onBoard(t, e, 0, manaFlareScript)
		forest := onBoard(t, e, 1, forestScript())
		if e.G.Obj(flare).Controller != 0 || e.G.Obj(forest).Controller != 1 {
			t.Fatalf("precondition: controllers are %d/%d, want 0/1",
				e.G.Obj(flare).Controller, e.G.Obj(forest).Controller)
		}
		// Mana Flare triggers for any player's land; seat 1 taps its own
		// Forest and the reflect pays seat 1 (Defined$ TriggeredActivator).
		e.pending = nil
		e.askPriority(1)
		start := len(e.L.Events)
		activateMana(t, e, forest)
		if got := e.G.Players[1].Pool[state.MG]; got != 2 {
			t.Fatalf("seat 1's Forest with Mana Flare green pool = %d, want 2 (the trigger is symmetric)", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("Mana Flare gave its controller seat 1's mana: %+v", e.G.Players[0].Pool)
		}
		if s := spanManaReflected(e, start); s.manaReflectNote {
			t.Fatalf("Mana Flare emitted the failure Note for seat 1: notes=%q", s.notes)
		}
	})

	t.Run("KinnanRockAddsColourless", func(t *testing.T) {
		e := handEngine(t)
		kinnan := onBoard(t, e, 0, kinnanTriggerScript)
		rock := onBoard(t, e, 0, manaRockScript)
		if e.G.Obj(kinnan).Zone != state.ZBattlefield || e.G.Obj(rock).Zone != state.ZBattlefield {
			t.Fatalf("precondition: Kinnan/Rock not on the battlefield (%v/%v)",
				e.G.Obj(kinnan).Zone, e.G.Obj(rock).Zone)
		}
		e.priorityRound()
		start := len(e.L.Events)
		activateMana(t, e, rock)
		if got := e.G.Players[0].Pool[state.MC]; got != 2 {
			t.Fatalf("Kinnan + {C} rock colourless pool = %d, want 2 (rock's {C} + the reflected {C})", got)
		}
		if s := spanManaReflected(e, start); s.manaReflectNote {
			t.Fatalf("Kinnan emitted the failure Note: notes=%q", s.notes)
		}
	})

	t.Run("KinnanForestDoesNotReflect", func(t *testing.T) {
		e := handEngine(t)
		kinnan := onBoard(t, e, 0, kinnanTriggerScript)
		forest := onBoard(t, e, 0, forestScript())
		if e.G.Obj(kinnan).Zone != state.ZBattlefield || e.G.Obj(forest).Zone != state.ZBattlefield {
			t.Fatalf("precondition: Kinnan/Forest not on the battlefield")
		}
		e.priorityRound()
		start := len(e.L.Events)
		activateMana(t, e, forest)
		if got := e.G.Players[0].Pool[state.MG]; got != 1 {
			t.Fatalf("Kinnan + Forest green pool = %d, want 1 only (ValidCard$ Permanent.nonLand excludes the land)", got)
		}
		if got := e.G.Players[0].Pool.Total(); got != 1 {
			t.Fatalf("Kinnan + Forest total pool = %d, want 1", got)
		}
		if s := spanManaReflected(e, start); s.manaReflectNote {
			t.Fatalf("Kinnan reflected a land it must not: notes=%q", s.notes)
		}
	})
}
