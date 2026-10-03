package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestClonePendingCastOwnsItsGrowingSlices pins that a clone taken mid-cast
// owns the pending cast's chosen targets, Fuse per-stage target slices and
// convoke taps. Each grows by append as the cast's asks are answered; with a
// shared backing array that has spare capacity, the clone's next answer
// would land in the very slot the original's next answer writes.
func TestClonePendingCastOwnsItsGrowingSlices(t *testing.T) {
	names, decks := testutil.SampleDecks(t, 2)
	e := New(Config{Seed: 3, Names: names, Decks: decks})
	e.Advance()
	drive(t, e, newTestBot(3), 10)
	targets := make([]state.Target, 1, 4)
	targets[0] = state.Target{Obj: 1}
	stage := make([]state.Target, 1, 4)
	stage[0] = state.Target{Obj: 2}
	stages := make([][]state.Target, 1, 4)
	stages[0] = stage
	convoke := make([]convokePayment, 1, 4)
	convoke[0] = convokePayment{id: 3}
	e.cast = &pendingCast{card: 1, mode: "fuse", targets: targets, stageTargets: stages, convoke: convoke}

	c := e.Clone()
	// The original answers first, then the clone answers differently.
	e.cast.targets = append(e.cast.targets, state.Target{Obj: 10})
	e.cast.stageTargets[0] = append(e.cast.stageTargets[0], state.Target{Obj: 20})
	e.cast.stageTargets = append(e.cast.stageTargets, []state.Target{{Obj: 21}})
	e.cast.convoke = append(e.cast.convoke, convokePayment{id: 30})
	c.cast.targets = append(c.cast.targets, state.Target{Obj: 11})
	c.cast.stageTargets[0] = append(c.cast.stageTargets[0], state.Target{Obj: 22})
	c.cast.stageTargets = append(c.cast.stageTargets, []state.Target{{Obj: 23}})
	c.cast.convoke = append(c.cast.convoke, convokePayment{id: 31})

	if got := e.cast.targets[1].Obj; got != 10 {
		t.Errorf("the clone's target answer overwrote the original's: targets[1] = %d, want 10", got)
	}
	if got := e.cast.stageTargets[0][1].Obj; got != 20 {
		t.Errorf("the clone's Fuse stage answer overwrote the original's: stageTargets[0][1] = %d, want 20", got)
	}
	if got := e.cast.stageTargets[1][0].Obj; got != 21 {
		t.Errorf("the clone's Fuse stage list overwrote the original's: stageTargets[1][0] = %d, want 21", got)
	}
	if got := e.cast.convoke[1].id; got != 30 {
		t.Errorf("the clone's convoke tap overwrote the original's: convoke[1] = %d, want 30", got)
	}
}
