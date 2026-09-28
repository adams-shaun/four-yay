package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestPumpSpellChangesPowerAndToughnessThroughTheLayerSystem is Task 19c's
// headline end-to-end test: it casts a real Giant-Growth-shaped Pump spell
// through the public engine surface (New/Advance/Submit, exactly as a client
// would) and asserts the target's Power/Toughness -- read through the layer
// system, not any internal field -- actually changed. This is what turns
// "the layer system exists and is unit-tested" into "a card that calls Pump
// really works," which is the whole reason this task exists: before this
// wiring, effPump only emitted a Note and the target's stats never moved.
func TestPumpSpellChangesPowerAndToughnessThroughTheLayerSystem(t *testing.T) {
	t.Parallel()
	growth := card(t, "Name:Giant Growth\nManaCost:G\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +3 | NumDef$ +3\nOracle:x\n")
	e := handEngine(t, growth)
	e.G.Players[0].Pool[state.MG] = 1

	bear := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	if got := e.Power(bear); got != 2 {
		t.Fatalf("power before the pump = %d, want 2", got)
	}

	e.askPriority(0)
	castFirst(t, e, "cast")

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the bear was not offered as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	// Everyone passes; the spell resolves.
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	if len(e.G.Stack) != 0 {
		t.Fatal("stack did not empty")
	}

	if got := e.Power(bear); got != 5 {
		t.Fatalf("power after the pump = %d, want 5 (2+3)", got)
	}
	if got := e.Toughness(bear); got != 5 {
		t.Fatalf("toughness after the pump = %d, want 5 (2+3)", got)
	}
}

// TestDismemberShapedPumpReducesToughness is the removal-spell shape the
// dispatch calls out by name: Dismember is "SP$ Pump | NumAtt$ -5 | NumDef$
// -5" in the real corpus (its {B/P}{B/P} Phyrexian cost is irrelevant to
// Pump's own behaviour, so this test uses a plain {1}{B} cost instead). A -5
// toughness swing is what actually kills most creatures it targets; asserting
// the drop is what proves this is real removal, not a logged no-op.
func TestDismemberShapedPumpReducesToughness(t *testing.T) {
	t.Parallel()
	dismember := card(t, "Name:Dismember\nManaCost:1 B\nTypes:Instant\n"+
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ -5 | NumDef$ -5\nOracle:x\n")
	e := handEngine(t, dismember)
	e.G.Players[0].Pool[state.MC] = 1
	e.G.Players[0].Pool[state.MB] = 1

	giant := onBoard(t, e, 1, "Name:Giant\nManaCost:4 R\nTypes:Creature Giant\nPT:5/5\nOracle:x\n")

	e.askPriority(0)
	castFirst(t, e, "cast")

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == giant {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("the giant was not offered as a target: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit target: %v", err)
	}
	for i := 0; i < 4 && len(e.G.Stack) > 0; i++ {
		castFirst(t, e, "pass")
	}
	if len(e.G.Stack) != 0 {
		t.Fatal("stack did not empty")
	}

	if got := e.Toughness(giant); got != 0 {
		t.Fatalf("toughness after Dismember = %d, want 0 (5-5)", got)
	}
	if got := e.Power(giant); got != 0 {
		t.Fatalf("power after Dismember = %d, want 0 (5-5)", got)
	}
}
