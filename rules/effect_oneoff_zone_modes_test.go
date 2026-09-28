package rules

// One-shot delayed-event registration tests.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// A OneOff$ True ChangesZone body registers WITHOUT the recurring |EF marker,
// so the FIRST matching battlefield-to-graveyard move consumes it: the second
// matching move fires nothing. Control for the recurring form is
// TestEffectChangesZoneTriggerRepeatsWithinTurn
// (rules/effect_event_modes_test.go).
func TestEffectOneOffChangesZoneConsumesOnFirstFiring(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:OneOffZone\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigZone\n"+
		"SVar:TrigZone:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card | OneOff$ True | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "ChangesZone" {
		t.Fatalf("precondition: Effect did not arm a ChangesZone registration: %+v", e.G.Delayed)
	}
	if e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: OneOff$ True body registered with the recurring |EF marker: %+v", e.G.Delayed[0])
	}
	first := onBoard(t, e, 0, "Name:First dying\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	second := onBoard(t, e, 0, "Name:Second dying\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if first == second || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatalf("precondition: want two distinct battlefield creatures, got %d and %d", first, second)
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
	e.emit(events.Event{Kind: events.MoveZone, Obj: first, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(first).Zone != state.ZGraveyard {
		t.Fatalf("precondition: the first creature did not move to the graveyard (zone %v)", e.G.Obj(first).Zone)
	}
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("first move did not resolve the one-shot body: life %d, want %d", got, before-2)
	}
	if n := delayedPushes(); n != 1 {
		t.Fatalf("first move fired the one-shot body %d times, want 1", n)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot ChangesZone registration survived its firing: %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: second, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(second).Zone != state.ZGraveyard {
		t.Fatalf("precondition: the second creature did not move to the graveyard (zone %v)", e.G.Obj(second).Zone)
	}
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if n := delayedPushes(); n != 1 {
		t.Fatalf("second move re-fired a consumed one-shot body (%d DelayedPush total)", n)
	}
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("second move resolved the consumed body: life %d, want %d", got, before-2)
	}
}

// ChangesController is admitted as a delayed-event mode, not as a printed
// trigger matcher. OneOff$ True must consume the registration after its first
// qualifying control change.
func TestEffectOneOffChangesControllerConsumesOnFirstFiring(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:OneOffControl\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigControl\n"+
		"SVar:TrigControl:Mode$ ChangesController | ValidCard$ Creature | ValidOriginalController$ You | OneOff$ True | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "ChangesController" || e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: one-shot ChangesController was not armed: %+v", e.G.Delayed)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "continuous effect trigger ChangesController unimplemented" {
			t.Fatal("ChangesController registration still failed closed")
		}
	}
	borrowed := onBoard(t, e, 0, "Name:Borrowed\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if obj := e.G.Obj(borrowed); obj.Zone != state.ZBattlefield || obj.Controller != 0 {
		t.Fatalf("precondition: creature starts in zone/controller %v/%d, want battlefield/0", obj.Zone, obj.Controller)
	}
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.ControlChange, Obj: borrowed, Player: 1})
	if obj := e.G.Obj(borrowed); obj.Controller != 1 {
		t.Fatalf("precondition: control change did not apply, controller = %d", obj.Controller)
	}
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if got := e.G.Players[0].Life; got != before-2 {
		t.Fatalf("qualifying controller change life = %d, want %d", got, before-2)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("one-shot registration survived firing: %+v", e.G.Delayed)
	}
}
