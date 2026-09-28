package rules

// This file pins the end-to-end real-card path the `modified` CardProperty
// (CR 700.9) unlocks: Arna Kennerüd, Skycaptain's own real trigger
//
//	T:Mode$ Attacks | ValidCard$ Creature.modified+YouCtrl |
//	TriggerZones$ Battlefield | Execute$ TrigDoubleCounters
//
// Previously `modified` was an unknown predicate, so the trigger never
// fired at all and Arna did nothing in a real game. This test drives the
// engine's own DeclareAttackers -> putTriggersOnStack -> resolveTop path
// for both halves of CR 700.9: the Equipment half (a token copy of the
// attached Equipment is created attached to the attacker) and the counter
// half (the attacker's counters double). A sibling test,
// TestArnaModifiedTriggerDoesNotFireUnmodified, is the negative that keeps
// the gate honest.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// arnaModifiedEngine builds the shared fixture: Arna, one Grizzly Bears to
// be the modified attacker, and one Bonesplitter to attach to it.
func arnaModifiedEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	arnaCard := lookup(t, reg, "Arna Kennerüd, Skycaptain")
	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{arnaCard, lookup(t, reg, "Grizzly Bears"), lookup(t, reg, "Bonesplitter")},
		[]*cards.Card{})
	arna := moveByName(t, e, 0, "Arna Kennerüd, Skycaptain", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	equip := moveByName(t, e, 0, "Bonesplitter", state.ZBattlefield)
	return e, cfg, arna, bear, equip
}

// TestArnaModifiedTriggerDoublesCounters is the counter half of CR 700.9:
// an unequipped attacker carrying a +1/+1 counter is modified, so Arna's
// trigger fires and MultiplyCounter doubles the counter.
func TestArnaModifiedTriggerDoublesCounters(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, arna, bear, equip := arnaModifiedEngine(t, reg)

	// Precondition: Arna really is on the battlefield, the attacker is a
	// creature controlled by player 0, and it starts with 2 counters. A
	// vacuous setup must fail here.
	if e.G.Obj(arna).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Arna zone=%s, want battlefield", e.G.Obj(arna).Zone)
	}
	if e.G.Obj(bear).Zone != state.ZBattlefield || e.G.Obj(bear).Controller != 0 {
		t.Fatalf("precondition failed: bear zone=%s controller=%d", e.G.Obj(bear).Zone, e.G.Obj(bear).Controller)
	}
	if e.G.Obj(equip).AttachedTo != 0 {
		t.Fatalf("precondition failed: Bonesplitter should be unattached for the counter half (AttachedTo=%d)", e.G.Obj(equip).AttachedTo)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 2})
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("precondition failed: bear P1P1=%d, want 2", got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("precondition failed: %d triggers already queued before the declaration", len(e.pendingTriggers))
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatalf("Arna's modified-creature Attacks trigger did not reach the stack")
	}
	e.resolveTop()

	if got := e.G.Obj(bear).Counter("P1P1"); got != 4 {
		t.Fatalf("attacker P1P1 after Arna's trigger = %d, want 4 (doubled from 2)", got)
	}
	replayCheck(t, e, cfg)
}

// TestArnaModifiedTriggerCopiesAttachedEquipment is the Equipment half of
// CR 700.9: an unequipped creature with no counters is NOT modified, so a
// Bonesplitter attached to the attacker is what makes it modified. Arna's
// trigger fires and creates a token copy of the Equipment attached to the
// attacker (her real AttachedTo$ TriggeredAttackerLKICopy endpoint).
func TestArnaModifiedTriggerCopiesAttachedEquipment(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, arna, bear, equip := arnaModifiedEngine(t, reg)

	// Give the attacker a counter too, so the counter rider has something to
	// double and its event is observable alongside the copy. The Equipment is
	// then the half under test: remove the counter is not possible, so this
	// test keeps both halves and the counter test above isolates the counter.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	e.emit(events.Event{Kind: events.Attach, Obj: equip, IDs: []state.ObjID{bear}})

	// Preconditions: the Equipment really is on the battlefield and attached
	// to the attacker, and the attacker really carries a counter.
	if e.G.Obj(equip).Zone != state.ZBattlefield || e.G.Obj(equip).AttachedTo != bear {
		t.Fatalf("precondition failed: equip zone=%s AttachedTo=%d, want battlefield attached to bear %d",
			e.G.Obj(equip).Zone, e.G.Obj(equip).AttachedTo, bear)
	}
	if e.G.Obj(bear).Counter("P1P1") != 1 {
		t.Fatalf("precondition failed: bear P1P1=%d, want 1", e.G.Obj(bear).Counter("P1P1"))
	}
	if e.G.Obj(arna).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Arna zone=%s, want battlefield", e.G.Obj(arna).Zone)
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) == 0 {
		t.Fatalf("Arna's modified-creature Attacks trigger did not reach the stack")
	}
	e.resolveTop()

	// The counter doubled.
	if got := e.G.Obj(bear).Counter("P1P1"); got != 2 {
		t.Fatalf("attacker P1P1 after Arna's trigger = %d, want 2 (doubled from 1)", got)
	}

	// The attached Equipment was copied and the copy entered attached to the
	// attacker.
	equipCard := e.G.Obj(equip).Card
	copyID := findTokenCopyOf(t, e, equipCard, equip)
	if e.G.Obj(copyID).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: the Equipment copy is not on the battlefield (zone %s)", e.G.Obj(copyID).Zone)
	}
	if got := e.G.Obj(copyID).AttachedTo; got != bear {
		t.Fatalf("the Equipment copy AttachedTo = %d, want the attacker %d", got, bear)
	}
	replayCheck(t, e, cfg)
}

// TestArnaModifiedTriggerDoesNotFireUnmodified is the negative half of the
// gate: an attacker with no counters and nothing attached is NOT modified,
// so Arna's trigger must not reach the stack. Without this the positive
// tests could pass on a trigger that fires unconditionally.
func TestArnaModifiedTriggerDoesNotFireUnmodified(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, arna, bear, equip := arnaModifiedEngine(t, reg)

	// Precondition: the attacker is unmodified and the Bonesplitter is
	// unattached, so this test's board really is the negative case.
	if len(e.G.Obj(bear).Counters) != 0 || e.G.Obj(bear).AttachedTo != 0 {
		t.Fatalf("precondition failed: bear counters=%v attachedTo=%d, want none", e.G.Obj(bear).Counters, e.G.Obj(bear).AttachedTo)
	}
	if e.G.Obj(equip).AttachedTo != 0 {
		t.Fatalf("precondition failed: Bonesplitter AttachedTo=%d, want unattached", e.G.Obj(equip).AttachedTo)
	}
	if e.G.Obj(arna).Zone != state.ZBattlefield {
		t.Fatalf("precondition failed: Arna zone=%s, want battlefield", e.G.Obj(arna).Zone)
	}

	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("Arna's trigger reached the stack for an UNMODIFIED attacker (%d stack entries)", len(e.G.Stack))
	}
	replayCheck(t, e, cfg)
}
