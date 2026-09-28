package rules

// effect_frame_trigger_test.go pins the brief's path 2: the one-shot
// self-exile idiom (`DB$ ChangeZone | Defined$ Self | Origin$ Command |
// Destination$ Exile`) run from an Effect's OWN Triggers$ body. Before this
// the delayed-trigger fire built a Ctx with no EffectFrame, so the idiom
// ended nothing and the Effect's registrations (its replacement/static
// half) lingered for their whole duration. Kor Dirge / Kor Chant carry the
// real corpus shape (`DB$ Effect | ReplacementEffects$ SelflessDamage |
// Triggers$ OutOfSight`, with `OutOfSight:Mode$ ChangesZone ... Execute$
// ExileEffect`); the fixture below is that shape with a DamageDone trigger
// so the test can fire it without a board move.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// effectTriggerSelfExileSrc is the Kor Dirge/Kor Chant lifetime shape: an
// Effect that registers a real continuous half (a keyword grant here, a
// damage replacement in the corpus cards) AND an Effect-created trigger
// whose Execute$ body is the Command-zone self-exile idiom.
const effectTriggerSelfExileSrc = "Name:Fixture Effect Trigger\nTypes:Creature\nPT:2/2\n" +
	"A:AB$ Effect | StaticAbilities$ Gift | Triggers$ Hook\n" +
	"SVar:Gift:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying\n" +
	"SVar:Hook:Mode$ DamageDone | ValidTarget$ You | Execute$ ExileEffect | TriggerZones$ Command\n" +
	"SVar:ExileEffect:DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile\n" +
	"Oracle:x\n"

// TestEffectTriggerBodySelfExileEndsTheEffect is the path-2 pin: the Effect
// registers a live continuous half, its own trigger fires, and resolving the
// trigger's self-exile body ends that registration. Without the trigger-path
// frame the registration survives (see the report's "Fails without the fix").
func TestEffectTriggerBodySelfExileEndsTheEffect(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	src := onBoard(t, e, 0, effectTriggerSelfExileSrc)
	if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: source not on battlefield: %+v", o)
	}
	face := e.G.Obj(src).Face()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])

	// Preconditions: the Effect's continuous half is live, and the
	// Effect-created trigger is registered with the recurring marker.
	if len(e.continuous) != 1 || !e.continuous[0].FromEffect || e.continuous[0].Source != src {
		t.Fatalf("precondition: continuous half = %+v, want one FromEffect registration from %d", e.continuous, src)
	}
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat || e.G.Delayed[0].Source != src {
		t.Fatalf("precondition: delayed trigger = %+v, want one EffectRepeat registration from %d", e.G.Delayed, src)
	}

	// Fire the Effect's own trigger: seat 0's damage satisfies ValidTarget$ You.
	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 1})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("precondition: the Effect trigger did not fire: %+v", e.pendingTriggers)
	}
	e.putTriggersOnStack()
	e.askPriority(0)
	passUntilStackEmpty(t, e, 8)

	if len(e.continuous) != 0 {
		t.Fatalf("the trigger body's self-exile did not end the Effect: %+v", e.continuous)
	}
}

// TestEffectTriggerBodySelfExileLeavesPrintedStatics is the negative
// control: a printed static registration of the SAME source (FromEffect
// false) must survive the Effect's source-scoped self-exile, so the ender
// cannot be mistaken for "end everything from this source".
func TestEffectTriggerBodySelfExileLeavesPrintedStatics(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	src := onBoard(t, e, 0, effectTriggerSelfExileSrc)
	// A printed static of the same source, marked not-Effect-created, as the
	// layer static scan would register it.
	e.continuous = append(e.continuous, state.ContinuousEffect{
		Source: src, Timestamp: 3, Controller: 0, Affects: "Card.Self", AddKeywords: []string{"Shroud"},
	})
	face := e.G.Obj(src).Face()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])

	has := func(kw string) bool {
		for _, ce := range e.continuous {
			if ce.Source == src && !ce.FromEffect && len(ce.AddKeywords) == 1 && ce.AddKeywords[0] == kw {
				return true
			}
		}
		return false
	}
	if !has("Shroud") || len(e.continuous) != 2 {
		t.Fatalf("precondition: expected the printed static plus the Effect half, got %+v", e.continuous)
	}

	e.emit(events.Event{Kind: events.Damage, Player: 0, Amount: 1})
	e.putTriggersOnStack()
	e.askPriority(0)
	passUntilStackEmpty(t, e, 8)

	if !has("Shroud") {
		t.Fatalf("the source's printed static was ended by the Effect self-exile: %+v", e.continuous)
	}
	if len(e.continuous) != 1 {
		t.Fatalf("expected exactly the printed static to survive, got %+v", e.continuous)
	}
}

// TestEffectChainSelfExileEndsTheEffect pins the ordinary-chain path: a
// `DB$ Effect ... SubAbility$ ExileEffect` in ONE resolution registers the
// Effect and then self-exiles it from the same chain. Before this the chain
// had no frame at all (the Effect was registered by an ordinary spell
// ability, not a replacement body), so the idiom ended nothing.
func TestEffectChainSelfExileEndsTheEffect(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	e.pending = nil
	line := "Name:Fixture Chain Effect\nTypes:Sorcery\n" +
		"A:SP$ Effect | StaticAbilities$ Gift | SubAbility$ ExileEffect\n" +
		"SVar:Gift:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying\n" +
		"SVar:ExileEffect:DB$ ChangeZone | Defined$ Self | Origin$ Command | Destination$ Exile\n" +
		"Oracle:x\n"
	spell := e.G.AddObject(card(t, line), 0)
	spell.Zone = state.ZStack
	sa := spell.Face().SpellAbility()
	effects.Resolve(e, &effects.Ctx{Source: spell.ID, Controller: 0, SVars: spell.Face().SVars}, sa)

	if len(e.continuous) != 0 {
		t.Fatalf("the chained self-exile did not end the Effect: %+v", e.continuous)
	}

	// Control: the same Effect WITHOUT the chained self-exile keeps its
	// registration, proving the fixture's Gift half really registers (so the
	// zero above is the ender at work, not a registration that never
	// happened).
	ctrl := e.G.AddObject(card(t, "Name:Fixture Chain Effect Control\nTypes:Sorcery\n"+
		"A:SP$ Effect | StaticAbilities$ Gift\n"+
		"SVar:Gift:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying\n"+
		"Oracle:x\n"), 0)
	ctrl.Zone = state.ZStack
	csa := ctrl.Face().SpellAbility()
	effects.Resolve(e, &effects.Ctx{Source: ctrl.ID, Controller: 0, SVars: ctrl.Face().SVars}, csa)
	if len(e.continuous) != 1 || !e.continuous[0].FromEffect || e.continuous[0].Source != ctrl.ID {
		t.Fatalf("control: the registration without a self-exile must survive: %+v", e.continuous)
	}
}
