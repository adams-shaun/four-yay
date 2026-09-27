package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestLargeEliminationSweepDoesNotTripLivelockWatcher(t *testing.T) {
	const tokenCount = 60001
	e := New(Config{Seed: 1, Names: []string{"departing", "survivor"}})
	bear := card(t, "Name:Elimination Sweep Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	for i := 0; i < tokenCount; i++ {
		o := e.G.AddObject(bear, 0)
		o.Zone = state.ZBattlefield
		o.IsToken = true
	}
	if got := len(e.G.Objs); got < tokenCount {
		t.Fatalf("precondition: created %d objects, want at least %d", got, tokenCount)
	}
	for i := 0; i < tokenCount; i++ {
		o := &e.G.Objs[i]
		if !o.IsToken || o.Owner != 0 || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d is not a battlefield token owned by departing seat: %+v", o.ID, o)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			if lle, ok := r.(*LivelockError); ok {
				t.Fatalf("legitimate %d-object departure sweep tripped livelock watcher: %v", tokenCount, lle)
			}
			panic(r)
		}
	}()
	e.emit(events.Event{Kind: events.PlayerLost, Player: 0, Text: "test elimination"})
	e.checkStateBased()

	if !e.G.Players[0].Lost {
		t.Fatal("departing player was not eliminated")
	}
	for i := 0; i < tokenCount; i++ {
		if got := e.G.Objs[i].Zone; got != state.ZCeased {
			t.Fatalf("token %d remains in zone %s, want ceased", e.G.Objs[i].ID, got)
		}
	}
	if !e.G.Over {
		t.Fatal("game did not end after the only surviving player remained")
	}
}
