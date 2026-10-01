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
		from := len(e.L.Events)
		e.emit(ev)
		var kinds []string
		for _, x := range e.L.Events[from:] {
			kinds = append(kinds, x.Kind.String())
		}
		e.active()
		if got := e.staticBuildSeq != seq; got != rescan {
			t.Fatalf("%s: rescanned = %v, want %v (events %v)", name, got, rescan, kinds)
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

// TestStaticMemoFineAdmissions pins static_memo_admit.go: a permanent whose
// only statics are not Mode$ Continuous enters a quiet board without a
// rescan, an object-local Choose on a static-cold object re-stamps while the
// same Choose on the static's own host (its AddType$ ChosenType reads it)
// rescans, and a scratch invalidation keeps a memo gated only by a known
// read set but drops one with an IsPresent$ gate.
func TestStaticMemoFineAdmissions(t *testing.T) {
	e := layerEngine(t)
	e.G.Active = 0
	host := onBoardGrant(t, e, 0, "Name:Chosen lord\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddType$ ChosenType\nOracle:x\n")
	bear := onBoardGrant(t, e, 0, creatureSrc("Fine bear"))
	// A creature whose one static is a restriction, not a Continuous grant:
	// coarse-hot for the zone skip, inert for this scan.
	o := e.G.AddObject(card(t, "Name:Wall bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n"+
		"S:Mode$ CantAttack | ValidCard$ Card.Self\nOracle:x\n"), 0)
	o.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append(append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...), o.ID))
	e.staticEpoch, e.activeEpoch = -1, -1 // the eventless placement (layers_test.go's onBoard)
	e.active()
	if !e.staticMemoQuiet() {
		t.Fatalf("precondition: gated %v, state-read %v", e.staticMemoGated, e.staticMemoStateRead)
	}
	step := func(name string, ev events.Event, rescan bool) {
		t.Helper()
		seq := e.staticBuildSeq
		e.emit(ev)
		e.active()
		if got := e.staticBuildSeq != seq; got != rescan {
			t.Fatalf("%s: rescanned = %v, want %v", name, got, rescan)
		}
	}
	if !objectStaticHotOn(o) || objectContinuousHot(o, state.ZBattlefield) {
		t.Fatal("precondition: the wall is not coarse-hot and continuous-cold")
	}
	step("non-Continuous static enters", events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield}, false)
	step("Choose on a static-cold object", events.Event{Kind: events.Choose, Obj: bear, Counter: "type", Text: "Elf"}, false)
	step("Choose on the static's host", events.Event{Kind: events.Choose, Obj: host, Counter: "type", Text: "Elf"}, true)
	if !slicesContainsString(e.Derived(bear).Types, "Elf") {
		t.Fatalf("chosen type not granted after the host's Choose: %v", e.Derived(bear).Types)
	}

	// Scratch invalidation: a PlayerTurn-gated memo survives it, an
	// IsPresent$-gated one does not.
	g := layerEngine(t)
	g.G.Active = 0
	onBoardGrant(t, g, 0, "Name:Turn lord\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | Condition$ PlayerTurn\nOracle:x\n")
	g.active()
	g.invalidateScratchLayerLists()
	if g.staticEpoch != len(g.L.Events) {
		t.Fatal("scratch invalidation dropped a memo gated only by the active player")
	}
	onBoardGrant(t, g, 0, "Name:Present lord\nTypes:Enchantment\n"+
		"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddPower$ 1 | IsPresent$ Creature.YouCtrl\nOracle:x\n")
	g.active()
	g.invalidateScratchLayerLists()
	if g.staticEpoch > 0 {
		t.Fatal("scratch invalidation kept a memo with an IsPresent$ gate")
	}
}

func slicesContainsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
