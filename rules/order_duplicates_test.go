package rules

// Forge's OrderDuplicates$ True on a T: line (Arcane Bombardment, Captured by
// the Consulate -- measured 2 corpus files) asks that duplicate instances of
// that trigger line be ordered as a block, so the copies' relative order among
// themselves is stable and none of them is interleaved with another trigger
// that fired simultaneously. groupOrderDuplicates implements that in the
// queue drain; these tests pin the queue order it produces, plus the control
// that a line WITHOUT the flag keeps its natural discovery order.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// ordDupSrc is the flagged shape: two permanent copies of Duper that fire on
// the same upkeep are duplicate instances of ONE trigger line, so they stay
// adjacent.
const ordDupSrc = `Name:Duper
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | OrderDuplicates$ True | Execute$ TrigGain | TriggerDescription$ gain 1 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`

// ordPlainSrc is the control: the same trigger line WITHOUT OrderDuplicates$,
// so its copies keep their natural discovery order.
const ordPlainSrc = `Name:Plain
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigGain | TriggerDescription$ gain 1 life
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`

// ordOtherSrc is the interleaving trigger: a distinct line that fires on the
// same upkeep and must not separate the two duplicate copies.
const ordOtherSrc = `Name:Other
ManaCost:B
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigDrain | TriggerDescription$ lose 1 life
SVar:TrigDrain:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`

// pendingSources returns the queue's leading entries' sources, so a test can
// assert the exact order the drain will offer.
func pendingSources(e *Engine) []state.ObjID {
	out := make([]state.ObjID, 0, len(e.pendingTriggers))
	for _, pt := range e.pendingTriggers {
		out = append(out, pt.Source)
	}
	return out
}

// TestOrderDuplicatesGroupsDuplicateInstancesAdjacently is the pin: with the
// flag, the two Duper copies are contiguous, in their stable discovery order,
// and the unrelated trigger is moved after them rather than between them. The
// precondition asserts all three really queued and are controlled by seat 0,
// so a vacuous setup (a trigger that never matched) fails loudly.
func TestOrderDuplicatesGroupsDuplicateInstancesAdjacently(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, ordDupSrc, ordOtherSrc, ordDupSrc)
	if len(e.pendingTriggers) != 3 {
		t.Fatalf("queued %d triggers, want 3 (dup, other, dup): %+v",
			len(e.pendingTriggers), pendingSources(e))
	}
	for _, pt := range e.pendingTriggers {
		if pt.Controller != 0 {
			t.Fatalf("trigger controlled by seat %d, want 0", pt.Controller)
		}
	}
	if ids[0] == ids[2] {
		t.Fatalf("the two Duper copies share an ObjID (%d); not two instances", ids[0])
	}
	if !e.putTriggersOnStack() {
		t.Fatal("three simultaneous triggers did not ask their controller for an order")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want a trigger-order decision", d)
	}
	// The two flagged copies sit at positions 0 and 1, in discovery order
	// (Duper #1 before Duper #2), and Other follows them.
	got := pendingSources(e)
	want := []state.ObjID{ids[0], ids[2], ids[1]}
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v (duplicates grouped at first occurrence)",
				got, want)
		}
	}
	// The order among the two copies is stable: the earlier-discovered copy
	// is still first.
	if posOf(e, ids[0]) > posOf(e, ids[2]) {
		t.Fatalf("duplicate copies reordered: %v", got)
	}
}

// TestOrderDuplicatesControlKeepsDiscoveryOrder is the control that makes the
// test above falsifiable: the SAME board without the flag leaves Other
// between the two Plain copies (their natural battlefield scan order), so the
// grouping above is genuinely the flag's doing and not an artefact of the
// queue.
func TestOrderDuplicatesControlKeepsDiscoveryOrder(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, ordPlainSrc, ordOtherSrc, ordPlainSrc)
	if len(e.pendingTriggers) != 3 {
		t.Fatalf("queued %d triggers, want 3: %+v", len(e.pendingTriggers), pendingSources(e))
	}
	if !e.putTriggersOnStack() {
		t.Fatal("three simultaneous triggers did not ask their controller for an order")
	}
	got := pendingSources(e)
	want := []state.ObjID{ids[0], ids[1], ids[2]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unflagged queue = %v, want discovery order %v", got, want)
		}
	}
}

// TestOrderDuplicatesSingleInstanceIsUntouched pins that the flag alone does
// nothing: one flagged trigger with no duplicate is left where it is (and,
// with only one trigger, no ordering ask is posed at all).
func TestOrderDuplicatesSingleInstanceIsUntouched(t *testing.T) {
	t.Parallel()
	e, ids := upkeepEngine(t, ordDupSrc)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued %d triggers, want 1: %+v", len(e.pendingTriggers), pendingSources(e))
	}
	if e.putTriggersOnStack() {
		t.Fatalf("a lone flagged trigger asked a decision: %+v", e.Pending())
	}
	// The drain placed the lone trigger on the stack unchanged (its queue
	// entry is consumed by the placement, so the stack, not the queue, holds
	// the result).
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Source != ids[0] {
		t.Fatalf("stack = %v, want the one trigger from %d", e.G.Stack, ids[0])
	}
}

func posOf(e *Engine, id state.ObjID) int {
	for i, pt := range e.pendingTriggers {
		if pt.Source == id {
			return i
		}
	}
	return -1
}
