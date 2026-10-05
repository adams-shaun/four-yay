package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// testRoomDoorCard builds a real two-door Enchantment Room (Forge's
// AlternateMode:Split, both halves Room-typed) so the door model -- cast face
// unlocked from entry, alternate face unlocked iff Unlocked -- is exercised.
func testRoomDoorCard(t *testing.T, front, back string) *cards.Card {
	t.Helper()
	script := "Name:" + front + "\nManaCost:2\nTypes:Enchantment Room\nOracle:x\nAlternateMode:Split\n\nALTERNATE\nName:" + back + "\nManaCost:3\nTypes:Enchantment Room\nOracle:y\n"
	card, diags := cards.ParseBytes("room.txt", []byte(script))
	if len(diags) != 0 {
		t.Fatalf("parse Room: %v", diags)
	}
	card.Link()
	return card
}

func countDoors(t *testing.T, h *fakeHost, controller state.PlayerID, expr string) int32 {
	t.Helper()
	got, ok := EvalCountOK(h, &Ctx{Controller: controller}, expr)
	if !ok {
		t.Fatalf("EvalCountOK(%q) did not resolve", expr)
	}
	return got
}

// TestCountUnlockedDoorsCountsFacesNotRooms pins CR 709.5j: a door is a HALF of
// a Room, so a Room that has only its cast face unlocked still contributes one
// door, and a fully unlocked Room contributes two. A reader that counted only
// the fully unlocked permanent (the bool as if it meant "has a door") would
// answer 3 here, not 7 -- the assertion below can fail.
func TestCountUnlockedDoorsCountsFacesNotRooms(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "Test Room", "Test Chamber")
	otherRoom := testRoomDoorCard(t, "Other Room", "Other Chamber")
	creature := mkCard(t, "Name:Test Creature\nManaCost:1\nTypes:Creature\nPT:1/1\nOracle:x\n")

	aID := h.g.AddObject(room, 0).ID        // cast face only: 1 door
	bID := h.g.AddObject(room, 0).ID        // fully unlocked: 2 doors, same names
	cID := h.g.AddObject(otherRoom, 0).ID   // fully unlocked: 2 doors, distinct names
	opponentID := h.g.AddObject(room, 1).ID // opponent's: 0
	crID := h.g.AddObject(creature, 0).ID   // not a Room: 0
	for _, id := range []state.ObjID{aID, bID, cID, opponentID, crID} {
		h.g.Obj(id).Zone = state.ZBattlefield
	}
	h.g.Obj(bID).Unlocked = true
	h.g.Obj(cID).Unlocked = true
	h.g.Obj(opponentID).Unlocked = true

	a := h.g.Obj(aID)
	if !a.Card.Faces[0].IsRoom() || !a.Card.Faces[1].IsRoom() {
		t.Fatal("precondition: both halves must be Room-typed")
	}
	if a.Unlocked {
		t.Fatal("precondition: the cast-only Room must NOT be fully unlocked")
	}
	if !h.g.Obj(bID).Unlocked || !h.g.Obj(cID).Unlocked {
		t.Fatal("precondition: the expected fully unlocked Rooms were not constructed")
	}
	if room.Faces[0].Name == otherRoom.Faces[0].Name {
		t.Fatal("precondition: the two Rooms must have distinct door names")
	}

	// Controller 0: 1 (a cast) + 2 (b both) + 2 (c both) = 5 doors, and the
	// opponent's fully unlocked Room (2 doors) is excluded; a bool-counting
	// reader would answer 3. The distinct door names are Test Room, Test
	// Chamber, Other Room, Other Chamber = 4.
	if got := countDoors(t, h, 0, "Count$UnlockedDoors"); got != 5 {
		t.Errorf("Count$UnlockedDoors = %d, want 5 (door faces, not rooms)", got)
	}
	if got := countDoors(t, h, 0, "Count$DistinctUnlockedDoors"); got != 4 {
		t.Errorf("Count$DistinctUnlockedDoors = %d, want 4", got)
	}
	if got := countDoors(t, h, 1, "Count$UnlockedDoors"); got != 2 {
		t.Errorf("opponent's Count$UnlockedDoors = %d, want 2 (only own Rooms)", got)
	}
}

func doorUnlockEvents(h *fakeHost) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.DoorUnlock {
			out = append(out, e)
		}
	}
	return out
}

func noteEvents(h *fakeHost) []events.Event {
	var out []events.Event
	for _, e := range h.log {
		if e.Kind == events.Note && e.Text != "" {
			out = append(out, e)
		}
	}
	return out
}

// TestUnlockDoorUnlocksLockedTargetRoom is the Mode$ Unlock target path
// (Ghostly Keybearer): a locked target Room gets one DoorUnlock event and
// becomes fully unlocked; a second resolution on the now-fully-unlocked Room
// is a no-op.
func TestUnlockDoorUnlocksLockedTargetRoom(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "Unlock Test Room", "Unlock Test Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !o.Card.Faces[0].IsRoom() || o.Unlocked || !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: target must be a battlefield Room with a locked door")
	}
	ability := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "Unlock"}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, ability)
	if !h.g.Obj(id).Unlocked {
		t.Fatal("a locked target Room was not unlocked")
	}
	if got := doorUnlockEvents(h); len(got) != 1 {
		t.Fatalf("DoorUnlock events = %d, want exactly 1: %+v", len(got), h.log)
	}
	// Precondition for the repeat: the Room is now fully unlocked and has no
	// locked door.
	if roomHasLockedDoor(h.g.Obj(id), 0) {
		t.Fatal("precondition: the Room still has a locked door after unlocking")
	}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, ability)
	if got := doorUnlockEvents(h); len(got) != 1 {
		t.Fatalf("a second resolution on the fully unlocked Room emitted another DoorUnlock: %d", len(got))
	}
}

// TestUnlockDoorUnlocksLockedRoomAmongLockedAndFullyUnlocked covers the real
// Ghostly Dancers pool spelling `Room.YouCtrl+!FullyUnlocked`: the fully
// unlocked Room is excluded by the filter predicate, so the locked one is
// forced and unlocked.
func TestUnlockDoorUnlocksLockedRoomAmongLockedAndFullyUnlocked(t *testing.T) {
	h := newHost(t, 2)
	lockedCard := testRoomDoorCard(t, "Locked Room", "Locked Chamber")
	doneCard := testRoomDoorCard(t, "Done Room", "Done Chamber")
	lockedID := h.g.AddObject(lockedCard, 0).ID
	doneID := h.g.AddObject(doneCard, 0).ID
	h.g.Obj(lockedID).Zone = state.ZBattlefield
	h.g.Obj(doneID).Zone = state.ZBattlefield
	h.g.Obj(doneID).Unlocked = true

	// Precondition: the filter the card actually spells distinguishes the two.
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":    "Unlock",
		"Choices": "Room.YouCtrl+!FullyUnlocked",
	}}
	pool := cardChoices(h, &Ctx{Controller: 0}, sa, 0)
	if len(pool) != 1 || pool[0].Obj != lockedID {
		t.Fatalf("precondition: choices pool = %+v, want only the locked Room %d", pool, lockedID)
	}

	Resolve(h, &Ctx{Source: lockedID, Controller: 0}, sa)
	if !h.g.Obj(lockedID).Unlocked {
		t.Fatal("the locked Room in the Choice$ pool was not unlocked")
	}
	if h.g.Obj(doneID).Unlocked == false {
		t.Fatal("precondition: the fully unlocked Room was silently re-locked")
	}
	if got := doorUnlockEvents(h); len(got) != 1 || got[0].Obj != lockedID {
		t.Fatalf("DoorUnlock events = %+v, want exactly the locked Room %d", got, lockedID)
	}
}

// TestUnlockDoorLockOrUnlockOnFullyUnlockedLockAnswerIsLoud checks that the
// fully unlocked Room can lock its first door without locking the other.
func TestUnlockDoorLockOrUnlockOnFullyUnlockedLockAnswerIsLoud(t *testing.T) {
	h := newTapeHost(t, 1) // option 1 = "Lock a door"
	room := testRoomDoorCard(t, "Full Room", "Full Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	o.Unlocked = true
	if roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must already be fully unlocked")
	}
	ability := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "LockOrUnlock"}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, ability)
	if got := doorUnlockEvents(h.fakeHost); len(got) != 0 {
		t.Fatalf("a fully unlocked LockOrUnlock target emitted %d DoorUnlock event(s)", len(got))
	}
	if doorUnlocked(h.g.Obj(id), 0) || !doorUnlocked(h.g.Obj(id), 1) {
		t.Fatalf("lock must leave only the second door live: %+v", h.g.Obj(id))
	}
	if got := countDoors(t, h.fakeHost, 0, "Count$UnlockedDoors"); got != 1 {
		t.Fatalf("locked cast face still counted: %d", got)
	}
	if len(h.log) != 1 || h.log[0].Kind != events.DoorLock || h.log[0].Amount != 1 {
		t.Fatalf("lock events = %+v", h.log)
	}
}

// TestUnlockDoorLockOrUnlockUnlocksWhenALockedDoorExists is the productive
// half of the same shape: with a locked door present the instruction unlocks
// it.
func TestUnlockDoorLockOrUnlockUnlocksWhenALockedDoorExists(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "KtH Room", "KtH Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must have a locked door")
	}
	ability := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "LockOrUnlock"}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, ability)
	if !h.g.Obj(id).Unlocked {
		t.Fatal("Mode$ LockOrUnlock did not unlock the locked target")
	}
	if got := doorUnlockEvents(h); len(got) != 1 {
		t.Fatalf("DoorUnlock events = %d, want 1", len(got))
	}
}

// TestUnlockDoorMultipleLockedRoomsPicksDeterministically reaches the
// multi-candidate ask path: two locked Rooms are in the pool, so the effect
// poses a KChoose. With no tape/host the deterministic stand-in takes the
// first candidate in object-id order. The assertion can fail if the pool
// narrowing or id order changes.
func TestUnlockDoorMultipleLockedRoomsPicksDeterministically(t *testing.T) {
	h := newHost(t, 2)
	first := testRoomDoorCard(t, "First Room", "First Chamber")
	second := testRoomDoorCard(t, "Second Room", "Second Chamber")
	firstID := h.g.AddObject(first, 0).ID
	secondID := h.g.AddObject(second, 0).ID
	h.g.Obj(firstID).Zone = state.ZBattlefield
	h.g.Obj(secondID).Zone = state.ZBattlefield

	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":    "Unlock",
		"Choices": "Room.YouCtrl+!FullyUnlocked",
	}}
	// Precondition: the effect really has TWO candidates, so it must ask.
	pool := cardChoices(h, &Ctx{Controller: 0}, sa, 0)
	if len(pool) != 2 {
		t.Fatalf("precondition: choices pool = %+v, want both locked Rooms", pool)
	}
	if firstID > secondID {
		t.Fatalf("precondition: firstID %d should precede secondID %d", firstID, secondID)
	}

	Resolve(h, &Ctx{Source: firstID, Controller: 0}, sa)
	if !h.g.Obj(firstID).Unlocked {
		t.Fatal("the first locked Room (id order) was not chosen and unlocked")
	}
	if h.g.Obj(secondID).Unlocked {
		t.Fatal("the no-host stand-in unlocked more than one Room")
	}
	if got := doorUnlockEvents(h); len(got) != 1 || got[0].Obj != firstID {
		t.Fatalf("DoorUnlock events = %+v, want exactly the first Room %d", got, firstID)
	}
}

// TestUnlockDoorUnsupportedModeIsLoud guards the fail-loud contract for a mode
// the model does not know: no state change, but a Note naming it.
func TestUnlockDoorUnsupportedModeIsLoud(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "Mode Room", "Mode Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	ability := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{"Mode": "SomethingNew"}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, ability)
	if h.g.Obj(id).Unlocked {
		t.Fatal("an unsupported Mode$ must not unlock anything")
	}
	if notes := noteEvents(h); len(notes) != 1 {
		t.Fatalf("unsupported Mode$ must emit one loud Note, got %+v", h.log)
	}
}
