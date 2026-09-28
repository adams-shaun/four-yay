package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestHiddenPredatorsIsPresent2IsAnAndClause pins IsPresent$/IsPresent2$ as
// two independent clauses that must BOTH hold (real corpus Hidden Predators:
// "When an opponent controls a creature with power 4 or greater, if CARDNAME
// is an enchantment, CARDNAME becomes a 4/4 Beast creature").
//
// The earlier UNION reading fired the Mode$ Always trigger while Hidden
// Predators was merely an enchantment (no opposing power-4 creature at all),
// and once it was animated -- no longer an enchantment -- re-fired on the
// opposing creature alone after every resolution, stacking one Permanent
// Animate continuous effect per loop. cardfuzz batch3 lines 4/21 reached 500+
// active continuous effects and a 60s wall-clock "hang" in the layer walk.
func TestHiddenPredatorsIsPresent2IsAnAndClause(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg,
		[]*cards.Card{lookup(t, reg, "Hidden Predators")},
		[]*cards.Card{lookup(t, reg, "Craw Wurm")})
	hp := moveByName(t, e, 0, "Hidden Predators", state.ZBattlefield)
	pushes := func() int {
		return countEvents(e, func(ev events.Event) bool {
			return ev.Kind == events.TriggerPush && ev.Obj == hp
		})
	}

	// Only the IsPresent2$ half holds (it is an enchantment, but no opponent
	// controls a power-4 creature): the trigger must not fire.
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if n := pushes(); n != 0 {
		t.Fatalf("trigger fired %d time(s) with no opposing power-4 creature; want 0", n)
	}
	if e.IsCreature(hp) {
		t.Fatal("Hidden Predators animated with no opposing power-4 creature")
	}

	// Both halves hold: it fires once and becomes a 4/4.
	moveByName(t, e, 1, "Craw Wurm", state.ZBattlefield)
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)
	if n := pushes(); n != 1 {
		t.Fatalf("trigger fired %d time(s) once both clauses held; want 1", n)
	}
	d := e.Derived(hp)
	if !e.IsCreature(hp) || d.Power != 4 || d.Toughness != 4 {
		t.Fatalf("Hidden Predators after the trigger: creature=%v %d/%d, want a 4/4 creature",
			e.IsCreature(hp), d.Power, d.Toughness)
	}

	// Animated, it is no longer an enchantment, so the IsPresent2$ half
	// fails and the state trigger never re-fires on the Wurm alone.
	for i := 0; i < 5; i++ {
		e.priorityRound()
		passUntilStackEmpty(t, e, 20)
	}
	if n := pushes(); n != 1 {
		t.Fatalf("trigger re-fired after Hidden Predators stopped being an enchantment: %d pushes, want 1", n)
	}
}
