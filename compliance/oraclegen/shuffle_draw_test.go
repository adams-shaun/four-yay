package oraclegen

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func TestShuffleThenDrawUniformFixtureIgnoresBattlefield(t *testing.T) {
	face := &cards.Face{Oracle: "Shuffle your hand and graveyard into your library, then draw seven cards."}
	if !ShufflesThenDraws(face) {
		t.Fatal("precondition: shuffle-then-draw face was not recognized")
	}
	sc := Scenario{Setup: map[string]Seat{
		"p0": {Battlefield: []string{"Grizzly Bears"}, Hand: []string{"Weftwalking"}, Graveyard: []string{"Ornithopter"}},
		"p1": {Battlefield: []string{"Grizzly Bears"}},
	}}
	if !UniformShuffleSetup(&sc, "Weftwalking") {
		t.Fatal("uniform fixture rejected a single-name shuffle source")
	}
	want := Repeat("Ornithopter", fixtureDeckSize-3)
	if got := sc.Setup["p0"].Library; !reflect.DeepEqual(got, want) {
		t.Fatalf("p0 library = %v, want %d Ornithopters", got, len(want))
	}
}

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

func TestShuffleThenDrawReportsUnuniformizableSetup(t *testing.T) {
	sc := Scenario{Setup: map[string]Seat{
		"p0": {Hand: []string{"Forest"}, Graveyard: []string{"Ornithopter"}},
		"p1": {},
	}}
	if UniformShuffleSetup(&sc, "Spell") {
		t.Fatal("precondition: distinct shuffled names must require hand-count comparison")
	}
}
