package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestNonCastRoomOffersAlternateDoorFirst(t *testing.T) {
	room := card(t, "Name:Meat Locker\nManaCost:0\nTypes:Enchantment Room\nOracle:x\n"+
		"ALTERNATE\nName:Drowned Diner\nManaCost:0\nTypes:Enchantment Room\nOracle:y\nAlternateMode:Split\n")
	o := &state.Object{Card: room, FaceIdx: 0, Zone: state.ZBattlefield}
	if o.Zone != state.ZBattlefield || o.DoorUnlocked(0) || o.DoorUnlocked(1) {
		t.Fatalf("precondition: expected a battlefield non-cast Room with both doors locked: %+v", o)
	}
	got := roomLockedFace(o)
	if got == nil || got.Name != "Drowned Diner" {
		name := "<nil>"
		if got != nil {
			name = got.Name
		}
		t.Fatalf("non-cast Room's first unlock offer = %q, want Drowned Diner", name)
	}
}
