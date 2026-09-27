package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// An EVENT-MATCHED delayed registration whose inline trigger body carries an
// IsPresent$ condition (Stolen Uniform's "When you lose control of that
// Equipment this turn, if it's attached to a creature you control, unattach
// it"; Fight for the Throne's "if you control your commander") must evaluate
// the condition against the REGISTRATION's own captured set
// (state.DelayedTrigger.Remembered): Card.IsTriggerRemembered binds the
// registration capture as its referent, exactly the binding the Phase arm's
// delayedPresentGateHolds supplies. Before the capture rode the condition walk
// the generic triggerConditionHoldsAs evaluated the clause with no
// DelayedRemembered bound, effects' IsTriggerRemembered answered unknown, and
// the registration failed closed -- a qualifying control change never fired.
//
// These tests drive the real producer (effects.Resolve on a DB$ DelayedTrigger
// SA, not a hand-built DelayedRegister event), so the serialized inline body
// in effects/misc.go delayedTriggerBody is exercised as well as the
// rules-side gate. The Execute$ bodies avoid api:Unattach (still
// unregistered) and tap the DelayTriggerRemembered referent instead -- the
// observable stand-in that proves the body ran with the RIGHT capture.

// uniformPromiseSource is the registering card: its Trig SVar taps the
// registration's own capture through DelayTriggerRemembered.
const uniformPromiseSource = "Name:Uniform promise\nTypes:Creature\nPT:2/2\n" +
	"SVar:Trig:DB$ Tap | Defined$ DelayTriggerRemembered\nOracle:x\n"

// registerUniformDelayed resolves a DB$ DelayedTrigger SA the way Stolen
// Uniform's DBDelayTrig chain resolves, with captured as the chain's
// Remembered and extra trigger clauses appended by the caller.
func registerUniformDelayed(t *testing.T, e *Engine, src, captured state.ObjID, clauses map[string]string) {
	t.Helper()
	params := map[string]string{
		"Mode":                    "ChangesController",
		"Execute":                 "Trig",
		"ValidCard":               "Card.IsTriggerRemembered",
		"ValidOriginalController": "You",
	}
	for k, v := range clauses {
		params[k] = v
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: captured}}},
		&cards.SA{Kind: "DB", API: "DelayedTrigger", Params: params})
	if len(e.G.Delayed) != 1 {
		t.Fatalf("precondition: registrations = %d, want 1 (%+v)", len(e.G.Delayed), e.G.Delayed)
	}
	dt := e.G.Delayed[0]
	if dt.EventMode != "ChangesController" || dt.Source != src || dt.Controller != 0 {
		t.Fatalf("precondition: registration = %+v", dt)
	}
	if len(dt.Remembered) != 1 || dt.Remembered[0].Obj != captured {
		t.Fatalf("precondition: capture = %+v, want object %d", dt.Remembered, captured)
	}
}

// uniformFixture is the shared board: the registering promise (seat 0), an
// Equipment, and the bearer it is attached to. The Equipment starts under
// seat 1's control, so the steal (control to seat 0) and the give-back
// (control to seat 1) are two real control changes.
type uniformFixture struct {
	e         *Engine
	src       state.ObjID
	equipment state.ObjID
	bearer    state.ObjID
}

func uniformFixtureNew(t *testing.T, e *Engine, src state.ObjID, bearerOwner state.PlayerID) uniformFixture {
	t.Helper()
	equipment := onBoard(t, e, 1, "Name:Stolen blade\nTypes:Equipment\nOracle:x\n")
	bearer := onBoard(t, e, bearerOwner, "Name:Bearer\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: equipment, IDs: []state.ObjID{bearer}})
	if e.G.Obj(equipment).AttachedTo != bearer || e.G.Obj(equipment).Controller != 1 {
		t.Fatalf("precondition: equipment attach = %+v controller = %d, want attached to %d under seat 1",
			e.G.Obj(equipment).AttachedTo, e.G.Obj(equipment).Controller, bearer)
	}
	if e.G.Obj(bearer).Zone != state.ZBattlefield {
		t.Fatalf("precondition: bearer zone = %s, want battlefield", e.G.Obj(bearer).Zone)
	}
	return uniformFixture{e: e, src: src, equipment: equipment, bearer: bearer}
}

// stealAndReturn performs the two control changes the promise observes: seat 0
// takes the Equipment, then seat 1 takes it back. The give-back is the
// qualifying event (ValidOriginalController$ You names the pre-change
// controller, seat 0); the steal must NOT fire it.
func (f uniformFixture) stealAndReturn() {
	f.e.emit(events.Event{Kind: events.ControlChange, Obj: f.equipment, Player: 0})
	f.e.emit(events.Event{Kind: events.ControlChange, Obj: f.equipment, Player: 1})
}

func TestDelayedEventPresentGateCapturedAttachedFires(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, uniformPromiseSource)
	f := uniformFixtureNew(t, e, src, 0)
	registerUniformDelayed(t, e, src, f.equipment, map[string]string{
		"IsPresent": "Card.IsTriggerRemembered+AttachedTo Creature.YouCtrl",
	})
	// Fold assertion: the real producer serialized the condition into the
	// inline event body, so the gate below reads a replay-visible body.
	if !strings.Contains(e.G.Delayed[0].Trigger,
		"IsPresent$ Card.IsTriggerRemembered+AttachedTo Creature.YouCtrl") {
		t.Fatalf("precondition: inline body = %q", e.G.Delayed[0].Trigger)
	}

	// The steal (control to seat 0) has the wrong original controller; the
	// give-back qualifies, and the captured Equipment IS attached to a
	// creature you control -- the trigger must fire and the body must act on
	// the REGISTRATION's capture.
	f.stealAndReturn()
	if e.G.Obj(f.equipment).Controller != 1 {
		t.Fatalf("precondition: equipment controller after give-back = %d, want 1",
			e.G.Obj(f.equipment).Controller)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("qualifying control change with capture attached to your creature: pending=%d, want 1",
			len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatal("delayed execute was not pushed")
	}
	e.resolveTop()
	if !e.G.Obj(f.equipment).Tapped {
		t.Fatal("execute did not run on the registration's capture")
	}
}

func TestDelayedEventPresentGateCapturedAttachedHeldBack(t *testing.T) {
	e := layerEngine(t)
	src := onBoard(t, e, 0, uniformPromiseSource)
	// The captured Equipment is attached to seat 1's creature, so the
	// AttachedTo Creature.YouCtrl clause counts ZERO: the qualifying control
	// change must not fire, and the one-shot registration must survive it.
	f := uniformFixtureNew(t, e, src, 1)
	registerUniformDelayed(t, e, src, f.equipment, map[string]string{
		"IsPresent": "Card.IsTriggerRemembered+AttachedTo Creature.YouCtrl",
	})

	f.stealAndReturn()
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("capture attached to the wrong controller's creature fired: pending=%d",
			len(e.pendingTriggers))
	}
	if len(e.G.Delayed) != 1 {
		t.Fatalf("failed condition consumed the one-shot registration: delayed=%d", len(e.G.Delayed))
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %d, want 0", len(e.G.Stack))
	}

	// The failed gate is the ONLY blocker: once the same captured Equipment is
	// re-attached to a creature you control (a NEW seat-0 bearer; f.bearer is
	// seat 1's), the very next qualifying control change fires the
	// registration that stayed pending.
	bearer0 := onBoard(t, e, 0, "Name:Bearer mine\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Attach, Obj: f.equipment, IDs: []state.ObjID{bearer0}})
	if e.G.Obj(f.equipment).AttachedTo != bearer0 || e.G.Obj(bearer0).Controller != 0 {
		t.Fatalf("precondition: re-attach = %d controller of bearer = %d",
			e.G.Obj(f.equipment).AttachedTo, e.G.Obj(bearer0).Controller)
	}
	f.stealAndReturn()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("re-attached capture: pending=%d, want 1 (registration was alive)", len(e.pendingTriggers))
	}
}

func TestDelayedEventPresentGateDistinctEventObject(t *testing.T) {
	// The capture and the firing event's object are DISTINCT: the
	// registration remembers the Equipment, but the control change happens to
	// an unrelated creature. The condition must still read the CAPTURED
	// Equipment's attachment (true case), and the execute must act on the
	// registration's capture, not the event object.
	t.Run("capture reads through a different event object", func(t *testing.T) {
		e := layerEngine(t)
		src := onBoard(t, e, 0, uniformPromiseSource)
		f := uniformFixtureNew(t, e, src, 0)
		registerUniformDelayed(t, e, src, f.equipment, map[string]string{
			"IsPresent": "Card.IsTriggerRemembered+AttachedTo Creature.YouCtrl",
			"ValidCard": "Creature",
		})
		changed := onBoard(t, e, 0, "Name:Unrelated\nTypes:Creature\nPT:2/2\nOracle:x\n")
		if e.G.Obj(changed).Controller != 0 || changed == f.equipment {
			t.Fatalf("precondition: firing object = controller %d, want seat 0 and distinct from %d",
				e.G.Obj(changed).Controller, f.equipment)
		}
		e.emit(events.Event{Kind: events.ControlChange, Obj: changed, Player: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("condition over the CAPTURE held the body back: pending=%d", len(e.pendingTriggers))
		}
		ctx := e.pendingTriggers[0].Ctx
		if len(ctx.Remembered) != 1 || ctx.Remembered[0].Obj != changed {
			t.Fatalf("Ctx.Remembered = %+v, want the event object %d", ctx.Remembered, changed)
		}
		if len(ctx.DelayedRemembered) != 1 || ctx.DelayedRemembered[0].Obj != f.equipment {
			t.Fatalf("Ctx.DelayedRemembered = %+v, want the registration capture %d",
				ctx.DelayedRemembered, f.equipment)
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("delayed execute was not pushed")
		}
		e.resolveTop()
		if !e.G.Obj(f.equipment).Tapped || e.G.Obj(changed).Tapped {
			t.Fatalf("execute acted on the wrong object: capture=%v eventObject=%v",
				e.G.Obj(f.equipment).Tapped, e.G.Obj(changed).Tapped)
		}
	})
	t.Run("wrong-controller capture holds the body back", func(t *testing.T) {
		e := layerEngine(t)
		src := onBoard(t, e, 0, uniformPromiseSource)
		f := uniformFixtureNew(t, e, src, 1)
		registerUniformDelayed(t, e, src, f.equipment, map[string]string{
			"IsPresent": "Card.IsTriggerRemembered+AttachedTo Creature.YouCtrl",
			"ValidCard": "Creature",
		})
		changed := onBoard(t, e, 0, "Name:Unrelated\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.ControlChange, Obj: changed, Player: 1})
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("condition over the CAPTURE did not hold the body back: pending=%d",
				len(e.pendingTriggers))
		}
		if len(e.G.Delayed) != 1 {
			t.Fatalf("failed condition consumed the one-shot registration: delayed=%d", len(e.G.Delayed))
		}
	})
}

func TestDelayedEventPresentGateNoPresentClauseStillFires(t *testing.T) {
	// A registration whose inline body carries NO IsPresent$ (every
	// pre-existing event body, e.g. the keyword-minted shapes) must keep
	// firing ungated -- the binding is additive, never a new universal gate.
	e := layerEngine(t)
	src := onBoard(t, e, 0, uniformPromiseSource)
	f := uniformFixtureNew(t, e, src, 1)
	registerUniformDelayed(t, e, src, f.equipment, nil)
	if strings.Contains(e.G.Delayed[0].Trigger, "IsPresent") {
		t.Fatalf("precondition: body carries IsPresent$: %q", e.G.Delayed[0].Trigger)
	}
	// Nothing here satisfies a capture-aware clause anyway: the captured
	// Equipment is attached to seat 1's creature, so any accidental presence
	// gate would hold the fire back.
	f.stealAndReturn()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("no-PresentClause body did not fire: pending=%d", len(e.pendingTriggers))
	}
}

// thronePromiseSource registers the Fight for the Throne shape: a ChangesZone
// body whose IsPresent$ is the NON-capture predicate Card.IsCommander+YouOwn+
// YouCtrl. The clause must keep holding without any capture binding -- a
// binding that answered unknown for non-capture predicates would break it.
const thronePromiseSource = "Name:Throne promise\nTypes:Creature\nPT:2/2\n" +
	"SVar:Trig:DB$ GainLife | Defined$ You | LifeAmount$ 2\nOracle:x\n"

// throneEngine builds a Commander game (seat 0's commander starts in the
// command zone, NOT on the battlefield) plus the registering promise and a
// victim creature under seat 1 that the ChangesZone body watches.
func throneEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e, _ := commanderGame(t, 17, FormatCommander, 0, [][]string{
		{"Name:My commander\nManaCost:2G\nTypes:Legendary Creature Elf\nPT:3/3\nOracle:x\n"},
		{"Name:Other commander\nManaCost:1B\nTypes:Legendary Creature Zombie\nPT:2/2\nOracle:x\n"},
	})
	cmd := e.G.Zone(state.ZCommand, 0)[0]
	if o := e.G.Obj(cmd); o == nil || o.Face() == nil || o.Face().Name != "My commander" {
		t.Fatalf("precondition: command zone = %+v", e.G.Obj(cmd))
	}
	src := onBoard(t, e, 0, thronePromiseSource)
	return e, src, cmd
}

func registerThroneDelayed(t *testing.T, e *Engine, src, victim state.ObjID) {
	t.Helper()
	n := len(e.G.Delayed)
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
		Remembered: []state.Target{{Obj: victim}}},
		&cards.SA{Kind: "DB", API: "DelayedTrigger", Params: map[string]string{
			"Mode":        "ChangesZone",
			"Execute":     "Trig",
			"ValidCard":   "Card.IsTriggerRemembered",
			"Origin":      "Battlefield",
			"Destination": "Graveyard",
			"IsPresent":   "Card.IsCommander+YouOwn+YouCtrl",
		}})
	if len(e.G.Delayed) != n+1 || e.G.Delayed[n].EventMode != "ChangesZone" {
		t.Fatalf("precondition: registration = %+v", e.G.Delayed)
	}
	if !strings.Contains(e.G.Delayed[n].Trigger, "IsPresent$ Card.IsCommander+YouOwn+YouCtrl") {
		t.Fatalf("precondition: inline body = %q", e.G.Delayed[n].Trigger)
	}
}

func victimDies(t *testing.T, e *Engine, victim state.ObjID) {
	t.Helper()
	if e.G.Obj(victim).Zone != state.ZBattlefield || e.G.Obj(victim).Controller != 1 {
		t.Fatalf("precondition: victim zone=%s controller=%d, want battlefield under seat 1",
			e.G.Obj(victim).Zone, e.G.Obj(victim).Controller)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield, To: state.ZGraveyard})
}

func TestDelayedEventPresentGateCommanderCondition(t *testing.T) {
	t.Run("commander on the battlefield fires", func(t *testing.T) {
		e, src, cmd := throneEngine(t)
		victim := onBoard(t, e, 1, "Name:Victim\nTypes:Creature\nPT:2/2\nOracle:x\n")
		registerThroneDelayed(t, e, src, victim)
		// Field the commander through a logged move; YouCtrl needs seat 0's
		// control on the battlefield.
		e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZCommand, To: state.ZBattlefield})
		if e.G.Obj(cmd).Zone != state.ZBattlefield || e.G.Obj(cmd).Controller != 0 {
			t.Fatalf("precondition: commander zone=%s controller=%d",
				e.G.Obj(cmd).Zone, e.G.Obj(cmd).Controller)
		}
		life := e.G.Players[0].Life
		victimDies(t, e, victim)
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("commander on board, capture died: pending=%d, want 1", len(e.pendingTriggers))
		}
		e.putTriggersOnStack()
		if len(e.G.Stack) == 0 {
			t.Fatal("delayed execute was not pushed")
		}
		e.resolveTop()
		if got := e.G.Players[0].Life; got != life+2 {
			t.Errorf("execute life=%d, want %d", got, life+2)
		}
	})
	t.Run("no commander on board holds the fire, then a fielded commander fires it", func(t *testing.T) {
		e, src, cmd := throneEngine(t)
		victim := onBoard(t, e, 1, "Name:Victim\nTypes:Creature\nPT:2/2\nOracle:x\n")
		registerThroneDelayed(t, e, src, victim)
		if len(e.G.Delayed) != 1 {
			t.Fatalf("precondition: registrations = %d, want 1", len(e.G.Delayed))
		}
		// The commander is still in the command zone: the battlefield count of
		// Card.IsCommander+YouOwn+YouCtrl is ZERO and the death must not fire.
		if e.G.Obj(cmd).Zone == state.ZBattlefield {
			t.Fatal("precondition: commander must start off the battlefield")
		}
		victimDies(t, e, victim)
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("no commander on board fired: pending=%d", len(e.pendingTriggers))
		}
		if len(e.G.Delayed) != 1 {
			t.Fatalf("failed condition consumed the one-shot registration: delayed=%d", len(e.G.Delayed))
		}
		if len(e.G.Stack) != 0 {
			t.Fatalf("stack = %d, want 0", len(e.G.Stack))
		}
		// The failed gate is the only blocker: field the commander and a SECOND
		// capture's death fires the still-pending registration... and a fresh
		// registration remembering the second victim proves both directions
		// again under the fielded commander.
		e.emit(events.Event{Kind: events.MoveZone, Obj: cmd, From: state.ZCommand, To: state.ZBattlefield})
		if e.G.Obj(cmd).Zone != state.ZBattlefield {
			t.Fatalf("precondition: commander field failed, zone=%s", e.G.Obj(cmd).Zone)
		}
		victim2b := onBoard(t, e, 1, "Name:Victim three\nTypes:Creature\nPT:2/2\nOracle:x\n")
		registerThroneDelayed(t, e, src, victim2b)
		if len(e.G.Delayed) != 2 {
			t.Fatalf("precondition: registrations = %d, want 2", len(e.G.Delayed))
		}
		victimDies(t, e, victim2b)
		// The first registration (capture = the first victim, long dead) does
		// not match this zone change; only the fresh registration fires.
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("commander fielded, second capture died: pending=%d, want 1", len(e.pendingTriggers))
		}
	})
}

// presentZoneFixture registers a ChangesController body whose IsPresent$ counts
// the capture in a NAMED zone (PresentZone$ Exile), not the battlefield.
func presentZoneRegister(t *testing.T, e *Engine, src, captured state.ObjID, zone, cmp string) {
	t.Helper()
	clauses := map[string]string{"IsPresent": "Card.IsTriggerRemembered", "ValidCard": "Creature"}
	if zone != "" {
		clauses["PresentZone"] = zone
	}
	if cmp != "" {
		clauses["PresentCompare"] = cmp
	}
	registerUniformDelayed(t, e, src, captured, clauses)
	if !strings.Contains(e.G.Delayed[0].Trigger, "Card.IsTriggerRemembered") {
		t.Fatalf("precondition: inline body = %q", e.G.Delayed[0].Trigger)
	}
}

func TestDelayedEventPresentGatePresentZoneAndCompare(t *testing.T) {
	t.Run("PresentZone$ Exile counts the capture there", func(t *testing.T) {
		e := layerEngine(t)
		src := onBoard(t, e, 0, uniformPromiseSource)
		captured := inZoneCard(t, e, 0, state.ZExile, "Name:Exiled promise\nTypes:Sorcery\nOracle:x\n")
		witness := onBoard(t, e, 0, "Name:Witness\nTypes:Creature\nPT:2/2\nOracle:x\n")
		if e.G.Obj(captured).Zone != state.ZExile {
			t.Fatalf("precondition: capture zone = %s, want exile", e.G.Obj(captured).Zone)
		}
		presentZoneRegister(t, e, src, captured, "Exile", "")
		// A battlefield IsPresent count of the capture would be 0; only the
		// Exile-zone reading can fire this registration.
		e.emit(events.Event{Kind: events.ControlChange, Obj: witness, Player: 1})
		if e.G.Obj(witness).Controller != 1 {
			t.Fatalf("precondition: witness controller = %d, want 1", e.G.Obj(witness).Controller)
		}
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("capture in PresentZone$ Exile did not fire: pending=%d", len(e.pendingTriggers))
		}
	})
	t.Run("capture outside the zone holds the fire until it arrives", func(t *testing.T) {
		e := layerEngine(t)
		src := onBoard(t, e, 0, uniformPromiseSource)
		captured := onBoard(t, e, 0, "Name:Not exiled\nTypes:Creature\nPT:2/2\nOracle:x\n")
		witness := onBoard(t, e, 0, "Name:Witness\nTypes:Creature\nPT:2/2\nOracle:x\n")
		presentZoneRegister(t, e, src, captured, "Exile", "")
		e.emit(events.Event{Kind: events.ControlChange, Obj: witness, Player: 1})
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("capture not in Exile fired: pending=%d", len(e.pendingTriggers))
		}
		if len(e.G.Delayed) != 1 {
			t.Fatalf("failed condition consumed the one-shot registration: delayed=%d", len(e.G.Delayed))
		}
		// Move the SAME capture into Exile and the next matching event fires.
		e.emit(events.Event{Kind: events.MoveZone, Obj: captured, From: state.ZBattlefield, To: state.ZExile})
		if e.G.Obj(captured).Zone != state.ZExile {
			t.Fatalf("precondition: capture move failed, zone=%s", e.G.Obj(captured).Zone)
		}
		witness2 := onBoard(t, e, 0, "Name:Witness two\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.ControlChange, Obj: witness2, Player: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("capture now in Exile did not fire: pending=%d", len(e.pendingTriggers))
		}
	})
	t.Run("PresentCompare$ EQ1 discriminates against the GE1 default", func(t *testing.T) {
		// Two captures on the battlefield: the default GE1 compare would fire
		// at count 2, so EQ1 NOT firing proves PresentCompare$ was read; the
		// fire when the count drops to 1 proves the gate re-evaluates.
		e := layerEngine(t)
		src := onBoard(t, e, 0, uniformPromiseSource)
		capturedA := onBoard(t, e, 0, "Name:Capture A\nTypes:Creature\nPT:2/2\nOracle:x\n")
		capturedB := onBoard(t, e, 0, "Name:Capture B\nTypes:Creature\nPT:2/2\nOracle:x\n")
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0,
			Remembered: []state.Target{{Obj: capturedA}, {Obj: capturedB}}},
			&cards.SA{Kind: "DB", API: "DelayedTrigger", Params: map[string]string{
				"Mode":           "ChangesController",
				"Execute":        "Trig",
				"ValidCard":      "Creature",
				"IsPresent":      "Card.IsTriggerRemembered",
				"PresentCompare": "EQ1",
			}})
		if len(e.G.Delayed) != 1 || len(e.G.Delayed[0].Remembered) != 2 {
			t.Fatalf("precondition: registration = %+v", e.G.Delayed)
		}
		witness := onBoard(t, e, 0, "Name:Witness\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.ControlChange, Obj: witness, Player: 1})
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("count 2 fired under PresentCompare$ EQ1: pending=%d", len(e.pendingTriggers))
		}
		if len(e.G.Delayed) != 1 {
			t.Fatalf("failed condition consumed the one-shot registration: delayed=%d", len(e.G.Delayed))
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: capturedB, From: state.ZBattlefield, To: state.ZGraveyard})
		if e.G.Obj(capturedB).Zone != state.ZGraveyard || e.G.Obj(capturedA).Zone != state.ZBattlefield {
			t.Fatalf("precondition: captures after move: %s / %s",
				e.G.Obj(capturedB).Zone, e.G.Obj(capturedA).Zone)
		}
		witness2 := onBoard(t, e, 0, "Name:Witness two\nTypes:Creature\nPT:2/2\nOracle:x\n")
		e.emit(events.Event{Kind: events.ControlChange, Obj: witness2, Player: 1})
		if len(e.pendingTriggers) != 1 {
			t.Fatalf("count 1 did not fire under EQ1: pending=%d", len(e.pendingTriggers))
		}
	})
}
