package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The trigger-side Metalcraft$ named condition (task trig-attacks-metalcraft):
// a T: line carrying `Metalcraft$ True` fires only while its controller
// controls three or more artifacts -- the same census the cost-modifier gate
// (costConditionHolds), the Continuous static gate and the activation-legality
// gate (activationConditionOK) already read. Before the gate existed the
// trigger fired unconditionally.
//
// The real-card pin is Vedalken Humiliator: its TrigPump body is DB$ AnimateAll,
// implemented since the api-animateall task (effects/animateall_test.go +
// rules/animateall_test.go, which resolves the trigger end to end and pins the
// unread RemoveAllAbilities$ loud note); this test pins the GATE alone.

// TestVedalkenHumiliatorMetalcraftGate declares the Humiliator attacking with
// two artifacts controlled (no trigger) and then with three (one trigger).
func TestVedalkenHumiliatorMetalcraftGate(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	hum := onBoardCard(t, e, 0, corpusCard(t, "Vedalken Humiliator"))
	art := corpusCard(t, "Ornithopter") // Artifact Creature: counts for Metalcraft
	onBoardCard(t, e, 0, art)
	onBoardCard(t, e, 0, art)

	declare := func() {
		t.Helper()
		e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{hum}})
		e.putTriggersOnStack()
	}

	declare()
	if n := len(e.G.Stack); n != 0 {
		t.Fatalf("attack trigger fired with two artifacts (stack = %d), want none", n)
	}

	onBoardCard(t, e, 0, art)
	declare()
	if n := len(e.G.Stack); n != 1 {
		t.Fatalf("attack trigger did not fire with three artifacts (stack = %d)", n)
	}
}

// TestTriggerBareConditionMetalcraftGate pins the bare-Condition$ spelling of
// the same gate on a synthetic trigger line: no corpus trigger carries
// `Condition$ Metalcraft` today (the bare-Condition$ Metalcraft carriers are
// S: statics the Continuous gate already reads), so the pin is deliberately
// synthetic and keeps the two spellings from drifting apart.
func TestTriggerBareConditionMetalcraftGate(t *testing.T) {
	t.Parallel()
	src := `Name:Artificer
ManaCost:1
Types:Creature Artificer
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Condition$ Metalcraft | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +1
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	onBoard(t, e, 0, "Name:Ornithopter\nManaCost:0\nTypes:Artifact Creature\nPT:0/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Ornithopter\nManaCost:0\nTypes:Artifact Creature\nPT:0/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if n := len(e.G.Stack); n != 0 {
		t.Fatalf("Condition$ Metalcraft trigger fired with two artifacts (stack = %d)", n)
	}
	onBoard(t, e, 0, "Name:Ornithopter\nManaCost:0\nTypes:Artifact Creature\nPT:0/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if n := len(e.G.Stack); n != 1 {
		t.Fatalf("Condition$ Metalcraft trigger did not fire with three artifacts (stack = %d)", n)
	}
}
