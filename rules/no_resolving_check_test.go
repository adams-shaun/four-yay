// The NoResolvingCheck$ True trigger param (task param:trig:AttackersDeclared
// .NoResolvingCheck): a trigger carrying it is checked ONLY when it fires;
// the resolution-time CR 603.4 recheck must not re-verify the condition, so
// transient fire-time state that moved since (a bounced attacker, drained
// power) never fizzles the ability. Pinned end to end on the real corpus
// carrier Ugin's Mastery (pack-tactics trigger: attack with total power 6 or
// greater), with a no-param control on the same scenario proving the
// resolution recheck stays live for every other trigger.
//
// Ugin's Mastery's T: line:
//
//	T:Mode$ AttackersDeclared | ValidAttackers$ Creature.YouCtrl | Execute$
//	TrigState | TriggerZones$ Battlefield | CheckSVar$ PackTactics |
//	SVarCompare$ GE6 | NoResolvingCheck$ True
//
// Its body (DB$ SetState | Mode$ TurnFaceUp) needs the face-down
// turn-face-up subsystem, which is a separate ticket; what this file pins is
// the param's read at the ONE resolution-side recheck site (resolveTop).
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const noResolvingCheckFizzleText = "fizzled: intervening-if no longer holds"

// fizzleMove finds the trigger object's resolution-time fizzle move, if any.
func fizzleMove(t *testing.T, e *Engine, trigID state.ObjID) *events.Event {
	t.Helper()
	for i, ev := range e.L.Events {
		if ev.Obj == trigID && ev.Kind == events.MoveZone &&
			ev.From == state.ZStack && ev.To == state.ZExile &&
			strings.Contains(ev.Text, noResolvingCheckFizzleText) {
			return &e.L.Events[i]
		}
	}
	return nil
}

// attackWithSixPower builds the shared fixture: the given enchantment (the
// corpus card or the control's inline twin without the param) on seat 0 with
// a 6-power Craw Wurm attacking seat 1, the pack-tactics trigger queued (its
// fire-time CheckSVar$ read held at exactly 6) and placed on the stack. The
// returned id is the stack object carrying the trigger.
func attackWithSixPower(t *testing.T, mastery *cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	e := combatEngine(t)
	m := onBoardCard(t, e, 0, mastery)
	wurm := onBoardCard(t, e, 0, mustCorpusCard(t, testutil.CorpusRegistry(t), "Craw Wurm"))
	e.G.Obj(wurm).SummonSick = false // the onBoardReady readiness, for a compiled card
	if e.Derived(wurm).Power < 6 {
		t.Fatalf("precondition: attacker power = %d, want >= 6", e.Derived(wurm).Power)
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{wurm}})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("precondition: fire-time pack-tactics check queued %d triggers, want 1 "+
			"(the GE6 read must hold while the wurm is still attacking)", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	trigID := state.ObjID(0)
	for _, sid := range e.G.Stack {
		if o := e.G.Obj(sid); o != nil && o.Source == m && o.Ability != nil {
			trigID = sid
		}
	}
	if trigID == 0 {
		t.Fatal("precondition: the queued trigger is not on the stack")
	}
	return e, trigID
}

// bounceTheAttacker removes the only attacker from the battlefield -- the
// transient state the fire-time CheckSVar$ counted, now gone (PackTactics
// reads 0 at resolution).
func bounceTheAttacker(t *testing.T, e *Engine, wurm state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: wurm,
		From: state.ZBattlefield, To: state.ZHand, Text: "bounced"})
	if o := e.G.Obj(wurm); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: bounced attacker in zone %v, want hand",
			o != nil)
	}
}

// TestNoResolvingCheckUginMasteryResolvesAfterPowerLeaves: the trigger's
// total attacking power drops below 6 AFTER the trigger is on the stack (the
// Craw Wurm is bounced to its owner's hand), and the trigger must still
// RESOLVE -- NoResolvingCheck$ True opts it out of the CR 603.4
// resolution-time recheck.
func TestNoResolvingCheckUginMasteryResolvesAfterPowerLeaves(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, trigID := attackWithSixPower(t, mustCorpusCard(t, reg, "Ugin's Mastery"))

	bounceTheAttacker(t, e, theCrawWurm(e))
	e.resolveTop()

	if ev := fizzleMove(t, e, trigID); ev != nil {
		t.Fatalf("NoResolvingCheck$ trigger fizzled at resolution: %q", ev.Text)
	}
	resolved := false
	for _, ev := range e.L.Events {
		if ev.Obj == trigID && ev.Kind == events.Resolve {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("NoResolvingCheck$ trigger did not resolve (no Resolve event on the trigger object)")
	}
}

// TestNoResolvingCheckControlWithoutTheParamStillFizzles: the same scenario
// on an inline twin of Ugin's Mastery WITHOUT the param -- the CR 603.4
// resolution-time recheck stays live for it, and it fizzles exactly as
// before. This control is what makes the first test prove the param, not a
// global disabling of the recheck.
func TestNoResolvingCheckControlWithoutTheParamStillFizzles(t *testing.T) {
	t.Parallel()
	twin, ds := cards.ParseBytes("twin", []byte("Name:Pack Tactics Twin\nManaCost:4\nTypes:Enchantment\n"+
		"T:Mode$ AttackersDeclared | ValidAttackers$ Creature.YouCtrl | Execute$ TrigPump | "+
		"TriggerZones$ Battlefield | CheckSVar$ PackTactics | SVarCompare$ GE6 | "+
		"TriggerDescription$ Whenever you attack with power 6 or greater, pump this.\n"+
		"SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ 1\n"+
		"SVar:PackTactics:Count$Valid Creature.attacking$CardPower\n"))
	if len(ds) != 0 {
		t.Fatalf("parse control twin: %v", ds)
	}
	if ds = twin.Link(); len(ds) != 0 {
		t.Fatalf("link control twin: %v", ds)
	}
	e, trigID := attackWithSixPower(t, twin)

	bounceTheAttacker(t, e, theCrawWurm(e))
	e.resolveTop()

	ev := fizzleMove(t, e, trigID)
	if ev == nil {
		t.Fatal("control without NoResolvingCheck$ did not fizzle -- the resolution " +
			"recheck this ticket's fix scopes is not live")
	}
	resolved := false
	for _, r := range e.L.Events {
		if r.Obj == trigID && r.Kind == events.Resolve {
			resolved = true
		}
	}
	if resolved {
		t.Fatal("control fizzled but also resolved; the assertions contradict")
	}
}

// theCrawWurm finds the shared fixture's attacking Craw Wurm.
func theCrawWurm(e *Engine) state.ObjID {
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Craw Wurm" {
			return id
		}
	}
	return 0
}
