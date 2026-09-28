package rules

// OneOff$ True on an api:Effect's Triggers$ body means the body is CONSUMED by
// its firing (CR 603.7's "when you next ..." promise), not a recurring trigger
// for the Effect's lifetime. The generic effEffect path must honour it for
// every registration mode with a non-repeat delayed dispatch -- not just
// SpellCast/ChangesZone. These pin the two modes the earlier round left
// recurring: the generic matcher arm (DamageDone and AttackersDeclared) and
// the Phase arm (inherently one-shot). The SpellCast
// opening-hand case is covered in rules/effect_trigger_owner_generic_test.go;
// recurring controls for DamageDone are in rules/effect_event_modes_test.go.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A OneOff$ True DamageDone body registers WITHOUT the recurring |EF marker,
// so its first matching damage consumes it: the second damage must fire
// nothing. ValidTarget$ Player makes the assertion conditional only on the
// damage event, so the OneOff read is the sole variable under test.
func TestEffectOneOffDamageDoneConsumesOnFirstFiring(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:OneOffDamage\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigDamage\n"+
		"SVar:TrigDamage:Mode$ DamageDone | ValidTarget$ Player | OneOff$ True | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	// Precondition: the generic path armed the body, and the OneOff read made
	// it a NON-repeat registration (the whole point -- a recurring one would
	// pass the "second damage fires nothing" assertion only by accident, and
	// the control test already pins the recurring form).
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "DamageDone" {
		t.Fatalf("precondition: Effect did not arm a DamageDone registration: %+v", e.G.Delayed)
	}
	if e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: OneOff$ True body registered with the recurring |EF marker: %+v", e.G.Delayed[0])
	}

	delayedPushes := func() int {
		n := 0
		for _, ev := range e.L.Events {
			if ev.Kind == events.DelayedPush {
				n++
			}
		}
		return n
	}
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("first damage did not resolve the one-shot body: life %d, want %d", got, before-2)
	}
	if n := delayedPushes(); n != 1 {
		t.Fatalf("first damage fired the one-shot body %d times, want 1", n)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot DamageDone registration survived its firing: %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if n := delayedPushes(); n != 1 {
		t.Fatalf("second damage re-fired a consumed one-shot body (%d DelayedPush total)", n)
	}
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("second damage resolved the consumed body: life %d, want %d", got, before-2)
	}
}

// AttackersDeclared uses its own event matcher and consumes the registration
// through the same non-repeat delayed path.
func TestEffectOneOffAttackersDeclaredConsumesOnFirstFiring(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:OneOffAttackers\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigAttack\n"+
		"SVar:TrigAttack:Mode$ AttackersDeclared | ValidAttackers$ Creature | OneOff$ True | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "AttackersDeclared" || e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: Effect did not arm a one-shot AttackersDeclared registration: %+v", e.G.Delayed)
	}
	attacker := onBoard(t, e, 0, "Name:Declared attacker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if obj := e.G.Obj(attacker); obj == nil || obj.Zone != state.ZBattlefield || !e.IsCreature(attacker) {
		t.Fatalf("precondition: attacker %d is not a battlefield creature", attacker)
	}
	before := e.G.Players[0].Life
	for n := 1; n <= 2; n++ {
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{attacker}})
		e.putTriggersOnStack()
		passUntilStackEmpty(t, e, 8)
		if got := e.G.Players[0].Life; got != before-2 {
			t.Fatalf("after declaration %d, controller life = %d, want %d (one firing total)", n, got, before-2)
		}
		if n == 1 && len(e.G.Delayed) != 0 {
			t.Fatalf("one-shot AttackersDeclared registration survived first firing: %+v", e.G.Delayed)
		}
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("consumed AttackersDeclared registration returned: %+v", e.G.Delayed)
	}
}

// A Mode$ Phase Effect trigger is inherently one-shot: its registration
// carries no |EF and the Phase arm emits none, so the DelayedPush that fires
// it consumes it -- true whether or not the body says OneOff$. Pinning it
// here keeps the finding's "Phase arm stays recurring" from being assumed
// either way: the precondition asserts the one-shot shape, and the firing
// asserts the registration is gone afterwards.
func TestEffectOneOffPhaseIsOneShot(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:PhaseOneOff\nManaCost:U\nTypes:Sorcery\n"+
		"SVar:Grant:DB$ Effect | Triggers$ TrigUpkeep\n"+
		"SVar:TrigUpkeep:Mode$ Phase | Phase$ Upkeep | OneOff$ True | ValidPlayer$ You | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	id := e.G.Zone(state.ZHand, 0)[0]
	ctx := &effects.Ctx{Source: id, Controller: 0}
	effects.SetSVars(ctx, e.G.Obj(id).Face().SVars)
	effects.Resolve(e, ctx, cards.ResolveSVar(e.G.Obj(id).Face().SVars, "Grant"))
	if len(e.G.Delayed) != 1 {
		t.Fatalf("precondition: Effect did not arm a Phase registration: %+v", e.G.Delayed)
	}
	dt := e.G.Delayed[0]
	if dt.EventMode != "" || dt.EffectRepeat {
		t.Fatalf("precondition: a Phase registration must be the one-shot no-EventMode shape: %+v", dt)
	}
	// Drive the registered upkeep step for its owner (seat 0), then resolve.
	e.G.Active = 0
	before := e.G.Players[0].Life
	if n := stepTriggers(e, state.StepUpkeep, 0); n != 1 {
		t.Fatalf("upkeep triggers queued = %d, want the one Phase registration", n)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the Phase trigger", e.G.Stack)
	}
	e.resolveTop()
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("Phase trigger did not resolve: life %d, want %d", got, before-2)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot Phase registration survived its firing: %+v", e.G.Delayed)
	}
}
