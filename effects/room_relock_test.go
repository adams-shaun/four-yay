package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRoomRelockCastFaceThenUnlock exercises the case a single Unlocked bool
// cannot express: only the OTHER door stays unlocked after locking the cast door.
func TestRoomRelockCastFaceThenUnlock(t *testing.T) {
	h := newTapeHost(t, 1)
	card := testRoomDoorCard(t, "Front Door", "Back Door")
	id := h.g.AddObject(card, 0).ID
	h.g.Obj(id).Zone = state.ZBattlefield
	h.Emit(events.Event{Kind: events.DoorUnlock, Obj: id})
	if !doorUnlocked(h.g.Obj(id), 0) || !doorUnlocked(h.g.Obj(id), 1) {
		t.Fatal("precondition: both doors must start unlocked")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "LockOrUnlock", "ValidTgts": "Room.YouCtrl"}}
	ctx := &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}
	Resolve(h, ctx, sa)
	if len(h.served) != 1 {
		t.Fatalf("lock choice not served: %+v", h.served)
	}
	if doorUnlocked(h.g.Obj(id), 0) || !doorUnlocked(h.g.Obj(id), 1) {
		t.Fatal("lock cast face changed wrong door")
	}
	if roomHasLockedDoor(h.g.Obj(id), 0) != true {
		t.Fatal("locked cast door not available to unlock")
	}
	if got := countDoors(t, h.fakeHost, 0, "Count$UnlockedDoors"); got != 1 {
		t.Fatalf("unlocked count = %d, want 1", got)
	}
	if got := countDoors(t, h.fakeHost, 0, "Count$DistinctUnlockedDoors"); got != 1 {
		t.Fatalf("distinct count = %d, want 1", got)
	}
	filter := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "Unlock", "Choices": "Room.YouCtrl+!FullyUnlocked"}}
	pool := cardChoices(h, &Ctx{Controller: 0}, filter, 0)
	if len(pool) != 1 || pool[0].Obj != id {
		t.Fatalf("locked cast face absent from !FullyUnlocked pool: %+v", pool)
	}
	Resolve(h, &Ctx{Source: id, Controller: 0}, filter)
	if !doorUnlocked(h.g.Obj(id), 0) || !doorUnlocked(h.g.Obj(id), 1) {
		t.Fatal("cast face did not unlock")
	}
	if got := countDoors(t, h.fakeHost, 0, "Count$UnlockedDoors"); got != 2 {
		t.Fatalf("reunlocked count = %d", got)
	}
	if got := doorUnlockEvents(h.fakeHost); len(got) != 2 || got[1].Amount != 1 {
		t.Fatalf("reunlock events = %+v", got)
	}
}
