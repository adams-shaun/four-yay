package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestShuffleThenDrawRequiresCausalSequence(t *testing.T) {
	unrelated := &cards.Face{Oracle: "Search your library, then shuffle it. {1}, Sacrifice: Draw a card."}
	if ShufflesThenDraws(unrelated) {
		t.Fatal("unrelated later draw was classified as part of the shuffle")
	}
	linked := &cards.Face{Oracle: "Shuffle your hand and graveyard into your library, then draw seven cards.\nThe first spell each player casts may be free."}
	if !ShufflesThenDraws(linked) {
		t.Fatal("causally linked shuffle-then-draw was not recognized")
	}
}

func TestUniformShuffledZonesIgnoresBattlefield(t *testing.T) {
	sc := Scenario{Setup: map[string]Seat{
		"p0": {Battlefield: []string{"Grizzly Bears"}, Hand: []string{"Weftwalking"}, Graveyard: []string{"Ornithopter"}},
		"p1": {Battlefield: []string{"Grizzly Bears"}},
	}}
	if !UniformShuffledZones(&sc, "Weftwalking") {
		t.Fatal("a single shuffled name must admit a uniform library")
	}
	if got, want := sc.Setup["p0"].Library, Repeat("Ornithopter", fixtureDeckSize-3); !reflect.DeepEqual(got, want) {
		t.Fatalf("p0 library = %v, want %d Ornithopters", got, len(want))
	}
	if n := len(sc.Setup["p1"].Library); n != 0 {
		t.Fatalf("p1 shuffles nothing back; its library must keep the Wastes filler, got %d cards", n)
	}
}

func TestUniformShuffledZonesReportsMixedNames(t *testing.T) {
	sc := Scenario{Setup: map[string]Seat{
		"p0": {Hand: []string{"Spell", "Forest"}, Graveyard: []string{"Ornithopter"}},
	}}
	if UniformShuffledZones(&sc, "Spell") {
		t.Fatal("two distinct shuffled names must not be reported uniform")
	}
	if n := len(sc.Setup["p0"].Library); n != 0 {
		t.Fatalf("a mixed seat must be left alone, got library of %d", n)
	}
	sc = Scenario{Setup: map[string]Seat{"p0": {Graveyard: []string{"Ornithopter"}, LibraryTop: []string{"Forest"}}}}
	if UniformShuffledZones(&sc, "Spell") {
		t.Fatal("a fixed library top is shuffled into a mixed library and must not be reported uniform")
	}
}
