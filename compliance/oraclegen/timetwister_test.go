package oraclegen

import (
	"reflect"
	"testing"
)

func TestUniformShuffleLibrariesFillsASingleNameSeat(t *testing.T) {
	var fx Fixture
	fx.P0().Hand = []string{"Turtles in Time"}
	fx.P1().Graveyard = []string{"Grizzly Bears"}
	UniformShuffleLibraries(&fx, "Turtles in Time")
	if len(fx.P0().Library) != 0 {
		t.Fatalf("p0 has nothing to shuffle back; its library must keep the Wastes filler, got %d cards", len(fx.P0().Library))
	}
	if want := Repeat("Grizzly Bears", 39); !reflect.DeepEqual(fx.P1().Library, want) {
		t.Fatalf("p1 library = %d cards %v..., want 39 Grizzly Bears", len(fx.P1().Library), fx.P1().Library[:min(3, len(fx.P1().Library))])
	}
}

func TestUniformShuffleLibrariesUniformizesOnlyShuffledZones(t *testing.T) {
	var fx Fixture
	fx.P0().Hand = []string{"Turtles in Time", "Forest"}
	fx.P0().Battlefield = []string{"Llanowar Elves"}
	if !UniformShuffleLibraries(&fx, "Turtles in Time") {
		t.Fatal("one shuffled name must admit a uniform library")
	}
	if want := Repeat("Forest", fixtureDeckSize-3); !reflect.DeepEqual(fx.P0().Library, want) {
		t.Fatalf("p0 library = %v, want %d Forests", fx.P0().Library, len(want))
	}
}
