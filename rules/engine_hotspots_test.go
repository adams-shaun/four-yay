package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

var newEngineSink *Engine

func objectArenaConfig(t testing.TB) Config {
	t.Helper()
	c := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:{T}: Add {R}.\n")
	decks := make([][]*cards.Card, 4)
	for i := range decks {
		decks[i] = make([]*cards.Card, 60)
		for j := range decks[i] {
			decks[i][j] = c
		}
	}
	return Config{Seed: 73, Names: []string{"a", "b", "c", "d"}, Decks: decks}
}

func BenchmarkNewEngineObjectArena(b *testing.B) {
	cfg := objectArenaConfig(b)
	b.ReportAllocs()
	for range b.N {
		newEngineSink = New(cfg)
	}
	if got := len(newEngineSink.G.Objs); got != 240 {
		b.Fatalf("objects = %d, want 240", got)
	}
}

func TestNewEngineSizesInitialObjectArenaWithHeadroom(t *testing.T) {
	t.Parallel()
	e := New(objectArenaConfig(t))
	if got := len(e.G.Objs); got != 240 {
		t.Fatalf("objects = %d, want 240", got)
	}
	// The dealt cards plus objectHeadroom for the objects a game mints, so a
	// game's first token does not regrow the whole arena.
	if got, want := cap(e.G.Objs), 240+objectHeadroom; got != want {
		t.Fatalf("object capacity = %d, want initial card count 240 + headroom %d = %d", got, objectHeadroom, want)
	}
	for i, o := range e.G.Objs {
		if o.ID != state.ObjID(i+1) {
			t.Fatalf("object %d has ID %d", i, o.ID)
		}
	}
}
