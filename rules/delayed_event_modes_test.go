package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func delayedModeWatcher(t testing.TB) (*Engine, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Delayed watcher\nTypes:Creature Wizard\nPT:2/2\n"+
		"SVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n")
	return e, src
}

func registerInlineDelayed(t testing.TB, e *Engine, src state.ObjID, mode, clauses string) {
	t.Helper()
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0,
		Step: e.G.Step, Counter: "Trig", Text: mode + ":Mode$ " + mode + clauses})
	if len(e.G.Delayed) != 1 || e.G.Delayed[0].EventMode != mode {
		t.Fatalf("registration = %+v, want one %s registration", e.G.Delayed, mode)
	}
}

func TestDelayedTriggerExpiredRegistrationsCollectedOnTurnChange(t *testing.T) {
	t.Parallel()
	e, src := delayedModeWatcher(t)
	if e.G.Turn < 1 {
		t.Fatalf("precondition: turn = %d", e.G.Turn)
	}
	for _, mode := range []string{"SpellCast", "Phase"} {
		text := "End of Turn"
		if mode == "SpellCast" {
			text = "SpellCast:Mode$ SpellCast | Execute$ Trig"
		}
		e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0,
			Step: state.StepEnd, Counter: "Trig", Text: text + "|TT=" + strconv.Itoa(int(e.G.Turn))})
	}
	if len(e.G.Delayed) != 2 || e.G.Delayed[0].MaxTurn != e.G.Turn || e.G.Delayed[1].MaxTurn != e.G.Turn {
		t.Fatalf("precondition: expiry registrations = %+v, turn %d", e.G.Delayed, e.G.Turn)
	}
	before := len(e.L.Events)
	e.emit(events.Event{Kind: events.TurnChange, Player: 1, Amount: e.G.Turn + 1})
	if len(e.G.Delayed) != 0 {
		t.Fatalf("expired registrations survived turn change without matching events: %+v", e.G.Delayed)
	}
	removes := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.DelayedRemove {
			removes++
		}
	}
	if removes != 2 {
		t.Fatalf("removal events = %d, want 2", removes)
	}
}

func TestDelayedTriggerDeadRegistrationIsCollected(t *testing.T) {
	t.Parallel()
	e, src := delayedModeWatcher(t)
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0,
		Step: e.G.Step, Counter: "MissingSVar"})
	if len(e.G.Delayed) != 1 {
		t.Fatalf("precondition: delayed registration = %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.StepChange, Step: e.G.Step})
	if len(e.G.Delayed) != 0 {
		t.Fatalf("dead registration was not collected: %+v", e.G.Delayed)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedRemove {
			found = true
		}
	}
	if !found {
		t.Fatal("collection did not emit DelayedRemove")
	}
}

func TestDelayedTriggerDeadEventRegistrationCollectedBeforeMatch(t *testing.T) {
	e, src := delayedModeWatcher(t)
	registerInlineDelayed(t, e, src, "ChangesZone", " | ValidCard$ Creature")
	e.G.Obj(src).Face().SVars = map[string]string{}
	other := onBoard(t, e, 1, "Name:Artifact\nTypes:Artifact\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: other, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.G.Delayed) != 0 {
		t.Fatalf("missing Execute registration survived unmatched event: %+v", e.G.Delayed)
	}
}

func TestDelayedControlChangeNewControllerGate(t *testing.T) {
	t.Parallel()
	e, src := delayedModeWatcher(t)
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: e.G.Step,
		Counter: "Trig", Text: "ChangesController:Mode$ ChangesController | ValidCard$ Creature | ValidNewController$ You"})
	card := onBoard(t, e, 0, "Name:Transfer\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if e.G.Obj(card).Controller != 0 || e.G.Obj(card).Zone != state.ZBattlefield {
		t.Fatalf("precondition: transfer = %+v", e.G.Obj(card))
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: card, Player: 1})
	if len(e.pendingTriggers) != 0 || len(e.G.Delayed) != 1 {
		t.Fatalf("wrong new controller consumed registration: pending=%d delayed=%d", len(e.pendingTriggers), len(e.G.Delayed))
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: card, Player: 0})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("right new controller: pending=%d", len(e.pendingTriggers))
	}
}

func TestDelayedControlChangeExecuteUsesEventObject(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Delayed watcher\nTypes:Creature Wizard\nPT:2/2\nSVar:Trig:DB$ Tap | Defined$ TriggeredObjectLKICopy\nOracle:x\n")
	captured := onBoard(t, e, 0, "Name:Captured\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	changed := onBoard(t, e, 0, "Name:Changed\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if captured == changed || e.G.Obj(changed).Zone != state.ZBattlefield || e.G.Obj(captured).Tapped || e.G.Obj(changed).Tapped {
		t.Fatal("precondition: distinct untapped battlefield objects required")
	}
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: e.G.Step,
		Counter: "Trig", IDs: []state.ObjID{captured},
		Text: "ChangesController:Mode$ ChangesController | ValidCard$ Creature | Execute$ Trig"})
	if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 || e.G.Delayed[0].Remembered[0].Obj != captured {
		t.Fatalf("precondition: registered capture = %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: changed, Player: 1})
	// The two referents are distinct: Ctx.Remembered is the FIRING event's
	// object (what a printed trigger of this mode captures, and what every
	// ordinary Triggered* spelling reads), while the registration's own
	// capture rides DelayedRemembered for DelayTriggerRemembered.
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pending triggers = %d, want one", len(e.pendingTriggers))
	}
	ctx := e.pendingTriggers[0].Ctx
	if len(ctx.Remembered) != 1 || ctx.Remembered[0].Obj != changed {
		t.Fatalf("Ctx.Remembered = %+v, want the event object %d", ctx.Remembered, changed)
	}
	if len(ctx.DelayedRemembered) != 1 || ctx.DelayedRemembered[0].Obj != captured {
		t.Fatalf("Ctx.DelayedRemembered = %+v, want the registration capture %d",
			ctx.DelayedRemembered, captured)
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("delayed execute was not pushed")
	}
	e.resolveTop()
	if !e.G.Obj(changed).Tapped || e.G.Obj(captured).Tapped {
		t.Fatalf("execute tapped captured instead of event object: changed=%v captured=%v", e.G.Obj(changed).Tapped, e.G.Obj(captured).Tapped)
	}
}

func TestDelayedTriggerEventModesFire(t *testing.T) {
	t.Parallel()
	t.Run("ChangesZone", func(t *testing.T) {
		e, src := delayedModeWatcher(t)
		registerInlineDelayed(t, e, src, "ChangesZone", " | Origin$ Library | Destination$ Battlefield | ValidCard$ Creature")
		card := e.G.AddObject(card(t, "Name:Arrival\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 1)
		if card.Zone != state.ZLibrary {
			t.Fatalf("precondition: arrival zone = %s, want library", card.Zone)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: card.ID, From: state.ZLibrary, To: state.ZBattlefield})
		if card.Zone != state.ZBattlefield || len(e.pendingTriggers) != 1 {
			t.Fatalf("zone delayed trigger did not fire: zone=%s pending=%d", card.Zone, len(e.pendingTriggers))
		}
	})

	t.Run("ChangesController", func(t *testing.T) {
		e, src := delayedModeWatcher(t)
		registerInlineDelayed(t, e, src, "ChangesController", " | ValidCard$ Creature | ValidOriginalController$ Player | ValidPlayer$ You")
		wrong := onBoard(t, e, 1, "Name:Wrong player\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if e.G.Obj(wrong).Controller != 1 {
			t.Fatalf("precondition: wrong-player controller = %d, want 1", e.G.Obj(wrong).Controller)
		}
		e.emit(events.Event{Kind: events.ControlChange, Obj: wrong, Player: 0})
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("ValidPlayer$ admitted the wrong original controller: pending=%d", len(e.pendingTriggers))
		}
		card := onBoard(t, e, 0, "Name:Borrowed\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if e.G.Obj(card).Controller != 0 {
			t.Fatalf("precondition: controller = %d, want 0", e.G.Obj(card).Controller)
		}
		e.emit(events.Event{Kind: events.ControlChange, Obj: card, Player: 1})
		if e.G.Obj(card).Controller != 1 || len(e.pendingTriggers) != 1 {
			t.Fatalf("control-change delayed trigger did not fire: controller=%d pending=%d", e.G.Obj(card).Controller, len(e.pendingTriggers))
		}
	})

	t.Run("DamageDone", func(t *testing.T) {
		e, src := delayedModeWatcher(t)
		registerInlineDelayed(t, e, src, "DamageDone", " | ValidSource$ Creature | ValidTarget$ Player")
		e.dmgSrcOverride = src
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
		if e.G.Players[1].Life >= 20 || len(e.pendingTriggers) != 1 {
			t.Fatalf("damage delayed trigger did not fire: life=%d pending=%d", e.G.Players[1].Life, len(e.pendingTriggers))
		}
	})

	t.Run("AttackersDeclared", func(t *testing.T) {
		e, src := delayedModeWatcher(t)
		registerInlineDelayed(t, e, src, "AttackersDeclared", " | AttackingPlayer$ You | ValidAttackers$ Creature")
		attacker := onBoard(t, e, 0, "Name:Attacker\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		if !e.IsCreature(attacker) || e.G.Obj(attacker).Zone != state.ZBattlefield {
			t.Fatalf("precondition: attacker is not a battlefield creature")
		}
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("attackers-declared delayed trigger did not fire: pending=%d", len(e.pendingTriggers))
		}
	})
}

// The other half of the split TestDelayedControlChangeExecuteUsesEventObject
// pins: an event-matched registration's Execute reaches the REGISTRATION's
// own capture through Defined$ DelayTriggerRemembered, while the ordinary
// Triggered* spellings keep reading the firing event's object.
func TestDelayedControlChangeExecuteReadsRegistrationCapture(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Delayed watcher\nTypes:Creature Wizard\nPT:2/2\n"+
		"SVar:Trig:DB$ Tap | Defined$ DelayTriggerRemembered\nOracle:x\n")
	captured := onBoard(t, e, 0, "Name:Captured\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	changed := onBoard(t, e, 0, "Name:Changed\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if captured == changed || e.G.Obj(captured).Tapped || e.G.Obj(changed).Tapped ||
		e.G.Obj(captured).Zone != state.ZBattlefield {
		t.Fatal("precondition: distinct untapped battlefield objects required")
	}
	e.emit(events.Event{Kind: events.DelayedRegister, Obj: src, Player: 0, Step: e.G.Step,
		Counter: "Trig", IDs: []state.ObjID{captured},
		Text: "ChangesController:Mode$ ChangesController | ValidCard$ Creature | Execute$ Trig"})
	if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 1 ||
		e.G.Delayed[0].Remembered[0].Obj != captured {
		t.Fatalf("precondition: registered capture = %+v", e.G.Delayed)
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: changed, Player: 1})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("delayed execute was not pushed")
	}
	e.resolveTop()
	if !e.G.Obj(captured).Tapped || e.G.Obj(changed).Tapped {
		t.Fatalf("DelayTriggerRemembered tapped the wrong object: captured=%v changed=%v",
			e.G.Obj(captured).Tapped, e.G.Obj(changed).Tapped)
	}
}
