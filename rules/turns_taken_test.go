package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestTurnsTakenCacheTracksEmitCloneAndExternalLogGrowth(t *testing.T) {
	t.Parallel()
	e := &Engine{
		G:    state.NewGame([]string{"a", "b"}),
		L:    events.NewLog(41),
		rng:  newRNG(41),
		loop: newLivelockWatcher(nil),
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0})
	e.emit(events.Event{Kind: events.TurnChange, Player: 1})
	if got := e.TurnsTaken(1); got != 2 {
		t.Fatalf("turns for player 1 = %d, want 2", got)
	}
	if len(e.turnsTaken) != 2 || e.turnsTaken[0] != 1 || e.turnsTaken[1] != 2 || e.turnsTakenEpoch != len(e.L.Events) {
		t.Fatalf("cache = %v at %d, log len %d", e.turnsTaken, e.turnsTakenEpoch, len(e.L.Events))
	}

	clone := e.Clone()
	if got := clone.TurnsTaken(0); got != 1 {
		t.Fatalf("clone turns for player 0 = %d, want 1", got)
	}
	if &clone.turnsTaken[0] == &e.turnsTaken[0] {
		t.Fatal("clone aliases the original turn-count cache")
	}

	// Direct log growth models a test/replay constructor that did not use
	// Engine.emit. TurnsTaken must detect the stale epoch and rebuild once.
	e.L.Append(events.Event{Kind: events.TurnChange, Player: 0})
	if got := e.TurnsTaken(0); got != 2 {
		t.Fatalf("turns after external log growth = %d, want 2", got)
	}
}
