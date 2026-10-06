package cost

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestParseCostExileCtrlOrGrave(t *testing.T) {
	c := ParseCost("5 G ExileCtrlOrGrave<1/Cave.Other>")
	// ExileCtrlOrGrave is a NON-mana cost: the <1/Spec> count is cards exiled,
	// never a generic amount, so the printed {5}{G} parses to Generic 5 with
	// no phantom pip (the pre-fix parser charged one extra generic per part).
	if c.Generic != 5 || c.Colored[4] != 1 || c.XMin != 0 || len(c.Exile) != 1 || len(c.Unknown) != 0 {
		t.Fatalf("parsed cost = %+v", c)
	}
	p := c.Exile[0]
	wantZones := uint8((1 << state.ZBattlefield) | (1 << state.ZGraveyard))
	if p.N != 1 || p.Spec != "Cave.Other" || p.ZoneSet != wantZones {
		t.Fatalf("exile part = %+v; want one Cave.Other from battlefield or graveyard (zone set %d)", p, wantZones)
	}
}

// TestParseCostExileCtrlOrGraveMultiSlot pins the multi-slot Craft shape: each
// ExileCtrlOrGrave<N/Spec> head is one independent Exile part and none of them
// adds generic mana (Throne of the Grim Captain prints {4} for four slots, so
// the parsed Generic must be exactly 4, not 4+4).
func TestParseCostExileCtrlOrGraveMultiSlot(t *testing.T) {
	c := ParseCost("4 ExileCtrlOrGrave<1/Dinosaur.Other> ExileCtrlOrGrave<1/Merfolk.Other> ExileCtrlOrGrave<1/Pirate.Other> ExileCtrlOrGrave<1/Vampire.Other> Exile<1/CARDNAME>")
	if c.Generic != 4 {
		t.Fatalf("Generic = %d, want 4 (the four slots are non-mana)", c.Generic)
	}
	if len(c.Exile) != 5 {
		t.Fatalf("Exile parts = %d, want 4 slots + self-exile", len(c.Exile))
	}
	want := []string{"Dinosaur.Other", "Merfolk.Other", "Pirate.Other", "Vampire.Other", "CARDNAME"}
	for i, w := range want {
		if c.Exile[i].Spec != w {
			t.Fatalf("Exile[%d].Spec = %q, want %q", i, c.Exile[i].Spec, w)
		}
	}
}
