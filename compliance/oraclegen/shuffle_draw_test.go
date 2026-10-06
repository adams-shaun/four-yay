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

func TestShuffleThenDrawReportsUnuniformizableSetup(t *testing.T) {
	sc := Scenario{Setup: map[string]Seat{
		"p0": {Hand: []string{"Forest"}, Graveyard: []string{"Ornithopter"}},
		"p1": {},
	}}
	if UniformShuffleSetup(&sc, "Spell") {
		t.Fatal("precondition: distinct shuffled names must require hand-count comparison")
	}
}
