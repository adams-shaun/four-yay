package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestDestinationUnknownAdmitsNoZone(t *testing.T) {
	zones := []state.Zone{
		state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard,
		state.ZExile, state.ZStack, state.ZCommand, state.ZCeased,
		state.ZSideboard, state.ZPlanarDeck,
	}
	for _, text := range []string{"Ante", "PlanarDeck", "TopOfLibrary", "BottomOfLibrary", "Ante,PlanarDeck"} {
		t.Run(text, func(t *testing.T) {
			d := DestinationOf(text)
			for _, z := range zones {
				if d.Admits(z) {
					t.Errorf("Destination$ %q admits %v", text, z)
				}
			}
			if got := d.Zones(); got != 0 {
				t.Errorf("Destination$ %q Zones() = %#x, want 0", text, got)
			}
			if z, ok := d.SoleZone(); ok {
				t.Errorf("Destination$ %q SoleZone() = %v, true; want false", text, z)
			}
		})
	}
}

func TestDestinationKnownAndWildcardControls(t *testing.T) {
	zones := []state.Zone{
		state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard,
		state.ZExile, state.ZStack, state.ZCommand, state.ZCeased,
		state.ZSideboard, state.ZPlanarDeck,
	}
	graveyard := DestinationOf("Graveyard")
	if !graveyard.Admits(state.ZGraveyard) || graveyard.Admits(state.ZHand) {
		t.Fatal("precondition/control: Graveyard must admit graveyard and reject hand")
	}
	if got, want := graveyard.Zones(), uint32(1<<state.ZGraveyard); got != want {
		t.Fatalf("Graveyard Zones() = %#x, want %#x", got, want)
	}

	list := DestinationOf("Graveyard,Exile")
	if !list.Admits(state.ZGraveyard) || !list.Admits(state.ZExile) || list.Admits(state.ZHand) {
		t.Fatal("precondition/control: Graveyard,Exile must admit those two zones and reject hand")
	}
	if got, want := list.Zones(), uint32(1<<state.ZGraveyard|1<<state.ZExile); got != want {
		t.Fatalf("Graveyard,Exile Zones() = %#x, want %#x", got, want)
	}

	for _, text := range []string{"Any", "All"} {
		d := DestinationOf(text)
		for _, z := range zones {
			if !d.Admits(z) {
				t.Errorf("Destination$ %q does not admit %v", text, z)
			}
		}
	}

	empty := DestinationOf("")
	if !empty.IsEmpty() {
		t.Fatal("empty Destination$ must carry the empty marker")
	}
	for _, z := range zones {
		if empty.Admits(z) {
			t.Errorf("empty Destination$ admits %v", z)
		}
	}
}
