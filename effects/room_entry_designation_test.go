package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// designateRoomCastFace makes a synthetic fixture represent a resolved Room
// spell. Production derives this bit from MoveZone provenance.
func designateRoomCastFace(o *state.Object) {
	if o != nil {
		o.CastDoor = true
	}
}

// TestNonCastRoomDoorCount distinguishes non-cast entry from the cast control:
// a Room object with only FaceIdx set is not a cast designation.
func TestNonCastRoomDoorCount(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "Count Room", "Count Chamber")
	noncast := h.g.AddObject(room, 0).ID
	h.g.Obj(noncast).Zone = state.ZBattlefield
	if h.g.Obj(noncast).DoorUnlocked(0) || h.g.Obj(noncast).DoorUnlocked(1) {
		t.Fatal("precondition failed: non-cast Room unexpectedly has an unlocked door")
	}
	if got := countDoors(t, h, 0, "Count$UnlockedDoors"); got != 0 {
		t.Fatalf("non-cast Count$UnlockedDoors = %d, want 0", got)
	}
	cast := h.g.AddObject(room, 0).ID
	h.g.Obj(cast).Zone = state.ZBattlefield
	designateRoomCastFace(h.g.Obj(cast))
	if got := countDoors(t, h, 0, "Count$UnlockedDoors"); got != 1 {
		t.Fatalf("Count$UnlockedDoors = %d, want 1 (only the cast control)", got)
	}
}
