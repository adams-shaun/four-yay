package botpolicy

// The C8 leaf pins (task botcounter1): a counter cast is worth its mana
// only at a stack holding a FOREIGN spell. The commit point is the cast
// choice — once cast, the target ask's Min-1 leaves no exit and the bot
// counters its own play — so the rule is stated and enforced here, on the
// plain-data Board both adapter halves build identically. These tests are
// synthetic Board fixtures (no engine, no corpus): the facts the rule reads
// (Card.Counter, Board.Stack) are plain data, and the real-game behaviour
// the rule changes is measured over whole bot games in the task report.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// counterPriority builds a KPriority decision with the options the engine
// offers at a priority where a counter is castable: one "cast" per supplied
// option and a "pass", Min/Max 1 as legalActions always emits.
func counterPriority(objs ...state.ObjID) *decision.Decision {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1}
	for i, id := range objs {
		d.Options = append(d.Options, decision.Option{Index: i, Kind: "cast", Obj: id})
	}
	d.Options = append(d.Options, decision.Option{Index: len(d.Options), Kind: "pass"})
	return &d
}

// chosenKind returns the Kind of the option the intent chose ("" when
// clamp somehow produced an out-of-range choice, which is its own failure).
func chosenKind(d *decision.Decision, in decision.Intent) string {
	if len(in.Choices) != 1 || in.Choices[0] < 0 || in.Choices[0] >= len(d.Options) {
		return ""
	}
	return d.Options[in.Choices[0]].Kind
}

// TestCounterNotCastAtOwnSpellsOnlyStack is C8's refusing half: with only
// the deciding seat's OWN spell on the stack, a counter cast option is not
// chosen — the decision answers "pass" even though the counter is the only
// affordable cast and would otherwise win the C3 ranking outright (CMC 2
// beats nothing else offered). Before the rule, this Board cast the counter
// and the follow-up target ask had no exit but the bot's own spell.
func TestCounterNotCastAtOwnSpellsOnlyStack(t *testing.T) {
	b := Board{
		IsMain: false, // an instant-speed priority: only the counter is offered
		Cards: TableOf(map[state.ObjID]Card{
			10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
		}),
		Stack: []StackEntry{
			{ID: 90, Controller: 0, IsSpell: true}, // the seat's own spell, mid-resolution
		},
	}
	d := counterPriority(10)
	in := Decide(b, d, rng(1))
	if got := chosenKind(d, in); got != "pass" {
		t.Fatalf("own-spells-only stack: chose %q (option %d), want \"pass\" — a counter with no foreign spell to eat must not be cast", got, in.Choices)
	}
}

// TestCounterNotCastAtEmptyStack is C8's degenerate half: the same refusal
// at an empty stack (the shape a counter whose offer is not withheld by the
// engine's cast census would otherwise fall into).
func TestCounterNotCastAtEmptyStack(t *testing.T) {
	b := Board{
		IsMain: false,
		Cards: TableOf(map[state.ObjID]Card{
			10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
		}),
	}
	d := counterPriority(10)
	in := Decide(b, d, rng(1))
	if got := chosenKind(d, in); got != "pass" {
		t.Fatalf("empty stack: chose %q (option %d), want \"pass\"", got, in.Choices)
	}
}

// TestCounterCastAtForeignSpell is C8's preserving half: the same Board
// with a FOREIGN spell on the stack (any other seat's — the multiplayer
// shape included, seat 1's spell against seat 0's decision) casts the
// counter. Countering an opponent's spell is exactly the behaviour the rule
// exists to preserve, so the census must not over-reach.
func TestCounterCastAtForeignSpell(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stack []StackEntry
	}{
		{"one foreign spell", []StackEntry{{ID: 91, Controller: 1, IsSpell: true}}},
		{"own and foreign", []StackEntry{
			{ID: 90, Controller: 0, IsSpell: true},
			{ID: 91, Controller: 1, IsSpell: true},
		}},
		{"foreign ability above a foreign spell", []StackEntry{
			{ID: 92, Controller: 1, IsSpell: false},
			{ID: 91, Controller: 1, IsSpell: true},
		}},
	} {
		b := Board{
			IsMain: false,
			Cards: TableOf(map[state.ObjID]Card{
				10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
			}),
			Stack: tc.stack,
		}
		d := counterPriority(10)
		in := Decide(b, d, rng(1))
		if got := chosenKind(d, in); got != "cast" {
			t.Fatalf("%s: chose %q, want the counter cast (a foreign spell is a real target)", tc.name, got)
		}
	}
}

// TestOwnSpellStillCastableBesideCounter is the complement the refusal must
// not eat: at an own-spells-only stack, a NON-counter cast option is still
// ranked normally and chosen over the pass — C8 declines the counter, never
// the seat's other plays.
func TestOwnSpellStillCastableBesideCounter(t *testing.T) {
	b := Board{
		IsMain: false,
		Cards: TableOf(map[state.ObjID]Card{
			10: {CMC: 2, Castable: true, InstantSpeed: true, Counter: true},
			11: {Creature: true, Power: 2, Castable: true},
		}),
		Stack: []StackEntry{{ID: 90, Controller: 0, IsSpell: true}},
	}
	d := counterPriority(10, 11)
	in := Decide(b, d, rng(1))
	if in.Choices[0] != 1 {
		t.Fatalf("chose option %d, want the creature cast (option 1) — C8 declines only the counter", in.Choices[0])
	}
}
