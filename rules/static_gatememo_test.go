package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestStaticGateRecheckDependencyClasses pins static_gatememo.go: a gated,
// state-read-free memo is re-stamped across a quiet run whose kinds cannot
// write its gates' inputs, re-stamped after a re-check that finds every
// outcome unchanged, and rescanned the moment an outcome moves. Each step
// asserts the served list too; layerInertVerify (on in this binary) rescans
// on every re-stamp and re-evaluates every skipped gate.
func TestStaticGateRecheckDependencyClasses(t *testing.T) {
	const playerTurn = "Name:Turn lord\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | Condition$ PlayerTurn\nOracle:x\n"
	// Angel of Vitality's shape (inline): +2/+2 while you have 25 or more life.
	const lifeGate = "Name:Life lord\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddToughness$ 2 | CheckSVar$ Y | SVarCompare$ GE25\n" +
		"SVar:Y:Count$YourLifeTotal\nOracle:x\n"
	// Elenda's compare shape: life above the starting total.
	const lifeVsStart = "Name:Start lord\nTypes:Enchantment\n" +
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 3 | CheckSVar$ X | SVarCompare$ GTZ\n" +
		"SVar:X:Count$YourLifeTotal\nSVar:Z:Count$YourStartingLife/Plus.2\nOracle:x\n"

	e := layerEngine(t)
	e.G.Active = 0
	turn := onBoardGrant(t, e, 0, playerTurn)
	life := onBoardGrant(t, e, 0, lifeGate)
	start := onBoardGrant(t, e, 0, lifeVsStart)
	bear := onBoardGrant(t, e, 0, creatureSrc("Gate bear"))
	e.active()
	if !e.staticMemoGated || e.staticMemoStateRead {
		t.Fatalf("precondition: gated %v, state-read %v", e.staticMemoGated, e.staticMemoStateRead)
	}
	deps := map[state.ObjID]uint8{}
	for _, r := range e.staticGates {
		deps[r.source] = r.dep
	}
	if deps[turn] != staticGateDepActive || deps[life] != staticGateDepLife || deps[start] != staticGateDepLife {
		t.Fatalf("gate classes = %v, want turn active, both life gates life", deps)
	}
	served := func() int {
		n := 0
		for _, ce := range e.staticContinuous {
			if ce.Source == turn || ce.Source == life || ce.Source == start {
				n++
			}
		}
		return n
	}
	step := func(name string, ev events.Event, rescan bool, want int) {
		t.Helper()
		seq := e.staticBuildSeq
		e.emit(ev)
		e.active()
		if got := e.staticBuildSeq != seq; got != rescan {
			t.Fatalf("%s: rescanned = %v, want %v", name, got, rescan)
		}
		if got := served(); got != want {
			t.Fatalf("%s: memo serves %d gated effects, want %d", name, got, want)
		}
	}
	// Seat 0's turn, 20 life: only the PlayerTurn grant is live.
	if got := served(); got != 1 {
		t.Fatalf("initial memo serves %d gated effects, want 1", got)
	}
	step("tap (no gate input written)", events.Event{Kind: events.Tap, Obj: bear}, false, 1)
	step("opponent's life (re-checked, unchanged)", events.Event{Kind: events.LifeChange, Player: 1, Amount: 3}, false, 1)
	step("own life to 23 (Start lord turns on)", events.Event{Kind: events.LifeChange, Player: 0, Amount: 3}, true, 2)
	step("own life to 25 (Life lord turns on)", events.Event{Kind: events.LifeChange, Player: 0, Amount: 2}, true, 3)
	step("damage to opponent (re-checked, unchanged)", events.Event{Kind: events.Damage, Player: 1, Amount: 1}, false, 3)
	step("opponent's turn (PlayerTurn turns off)", events.Event{Kind: events.TurnChange, Player: 1}, true, 2)
	step("untap (no gate input written)", events.Event{Kind: events.Untap, Obj: bear}, false, 2)
}

// TestStaticGateRecordsTravelWithClone pins clone.go's carry of the gate
// records: a clone of a gated memo re-stamps across a quiet run instead of
// rescanning, and a clone without them never re-stamps blind.
func TestStaticGateRecordsTravelWithClone(t *testing.T) {
	e := layerEngine(t)
	e.G.Active = 0
	onBoardGrant(t, e, 0, "Name:Turn lord\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | Condition$ PlayerTurn\nOracle:x\n")
	bear := onBoardGrant(t, e, 0, creatureSrc("Clone bear"))
	e.active()
	c := e.Clone()
	if !c.staticGatesKnown || len(c.staticGates) != len(e.staticGates) || len(c.staticGates) == 0 {
		t.Fatalf("clone carried %d gate records (known %v), want %d", len(c.staticGates), c.staticGatesKnown, len(e.staticGates))
	}
	seq := c.staticBuildSeq
	c.emit(events.Event{Kind: events.Tap, Obj: bear})
	c.active()
	if c.staticBuildSeq != seq {
		t.Fatal("clone rescanned a gated memo across a tap")
	}
	c.staticGatesKnown = false
	c.emit(events.Event{Kind: events.Untap, Obj: bear})
	c.active()
	if c.staticBuildSeq == seq {
		t.Fatal("a memo without gate records was re-stamped across a quiet run")
	}
}
