package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Effect-created ChangesController registrations repeat while their |EF
// lifetime remains active. The matcher must honor both the event's original
// controller and the card filter; unrelated events cannot fire the body.
func TestEffectChangesControllerTriggerRepeats(t *testing.T) {
	t.Parallel()
	promise := card(t, "Name:ControlPromise\nManaCost:U\nTypes:Sorcery\n"+
		"A:SP$ Effect | Triggers$ TrigControl\n"+
		"SVar:TrigControl:Mode$ ChangesController | ValidCard$ Creature | ValidOriginalController$ You | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
	e := handEngine(t, promise)
	e.G.Players[0].Pool[state.MU] = 1
	e.askPriority(0)
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "ChangesController" || !e.G.Delayed[0].EffectRepeat {
		t.Fatalf("precondition: recurring ChangesController was not armed: %+v", e.G.Delayed)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "continuous effect trigger ChangesController unimplemented" {
			t.Fatal("ChangesController registration still failed closed")
		}
	}
	matching := onBoard(t, e, 0, "Name:Matching one\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	matching2 := onBoard(t, e, 0, "Name:Matching two\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	wrongController := onBoard(t, e, 1, "Name:Wrong original controller\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	unrelated := onBoard(t, e, 0, "Name:Unrelated land\nTypes:Land\nOracle:x\n")
	for _, id := range []state.ObjID{matching, matching2, wrongController, unrelated} {
		if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not on the battlefield: %+v", id, obj)
		}
	}
	if e.G.Obj(matching).Controller == e.G.Obj(wrongController).Controller {
		t.Fatal("precondition: original-controller comparison must differ")
	}
	before := e.G.Players[0].Life
	// Same card type but wrong original controller: no match.
	e.emit(events.Event{Kind: events.ControlChange, Obj: wrongController, Player: 0})
	// Right original controller but wrong card type: no match.
	e.emit(events.Event{Kind: events.ControlChange, Obj: unrelated, Player: 1})
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 8)
	if got := e.G.Players[0].Life; got != before {
		t.Fatalf("nonmatching controller/type fired the trigger: life %d, want %d", got, before)
	}
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat {
		t.Fatalf("nonmatching events consumed recurring registration: %+v", e.G.Delayed)
	}
	for _, id := range []state.ObjID{matching, matching2} {
		e.emit(events.Event{Kind: events.ControlChange, Obj: id, Player: 1})
		e.putTriggersOnStack()
		passUntilStackEmpty(t, e, 8)
	}
	if got, want := e.G.Players[0].Life, before-4; got != want {
		t.Fatalf("two qualifying controller changes life = %d, want %d", got, want)
	}
	if len(e.G.Delayed) != 1 || !e.G.Delayed[0].EffectRepeat {
		t.Fatalf("recurring registration did not survive its firings: %+v", e.G.Delayed)
	}
}

// The event matcher also evaluates Card.IsRemembered against the registration's
// captured objects. Exercise that capture through effEffect rather than a
// hand-built DelayedRegister event.
func TestEffectChangesControllerUsesRememberedCapture(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Remembering promise\nTypes:Enchantment\n"+
		"SVar:TrigControl:Mode$ ChangesController | ValidCard$ Card.IsRemembered | ValidOriginalController$ You | TriggerZones$ Command | Execute$ TrigPain\n"+
		"SVar:TrigPain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\\nOracle:x\\n")
	remembered := onBoard(t, e, 0, "Name:Remembered equipment\\nTypes:Equipment\\nOracle:x\\n")
	unremembered := onBoard(t, e, 0, "Name:Not remembered\\nTypes:Equipment\\nOracle:x\\n")
	if e.G.Obj(remembered).Zone != state.ZBattlefield || e.G.Obj(unremembered).Zone != state.ZBattlefield {
		t.Fatal("precondition: both Equipment objects must be on the battlefield")
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: remembered}}}, &cards.SA{Kind: "DB", API: "Effect", Params: map[string]string{
		"Triggers": "TrigControl", "Duration": "Permanent", "RememberObjects": "Remembered",
	}})
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != "ChangesController" || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != remembered {
		t.Fatalf("precondition: Effect did not register the remembered object: %+v", e.G.Delayed)
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
	e.emit(events.Event{Kind: events.ControlChange, Obj: unremembered, Player: 1})
	e.putTriggersOnStack()
	if n := delayedPushes(); n != 0 {
		t.Fatalf("unremembered Equipment fired promise: DelayedPush count %d, want 0", n)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: remembered, Player: 1})
	e.putTriggersOnStack()
	if n := delayedPushes(); n != 1 {
		t.Fatalf("remembered Equipment did not fire promise: DelayedPush count %d, want 1", n)
	}
}
