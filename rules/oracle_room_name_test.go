package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// A setup-placed Room enters with no door cast, so both doors are locked and
// its CR 709.5 name is the empty string -- the same name XMage's
// RoomCharacteristicsEffect reports for a Room whose every door is locked.
const roomPlacedBothLockedScenario = `{"name":"room-placed","cr":["709.5"],"why":"a setup-placed Room has both doors locked","setup":{"p0":{"battlefield":["Bottomless Pool"]}},"steps":[{"op":"pass_to","seat":0,"step":"begin-combat"}]}`

// Casting the front door unlocks it, so the permanent wears the front face's
// name. Bottomless Pool's "when you unlock this door" trigger fires on the
// cast and resolves with no target (TargetMin$ 0).
const roomFrontDoorScenario = `{"name":"room-front","cr":["709.5"],"why":"cast the front door","setup":{"p0":{"hand":["Bottomless Pool // Locker Room"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Bottomless Pool","mana":"U"},{"op":"resolve","seat":0}]}`

// Cast the front door, then pay the locked door's mana cost to unlock it: both
// doors are now unlocked, so the name joins both face names.
const roomBothDoorsScenario = `{"name":"room-both","cr":["709.5"],"why":"cast the front door then unlock the other","setup":{"p0":{"hand":["Bottomless Pool // Locker Room"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Bottomless Pool","mana":"U"},{"op":"resolve","seat":0},{"op":"activate","seat":0,"card":"p0:Bottomless Pool","ability":"Unlock Locker Room","mana":"UUUUU"}]}`

func TestOracleRoomPlacedBothLockedHasNoName(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(roomPlacedBothLockedScenario))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	seen := 0
	for i, s := range res.Snapshots {
		room, ok := snapPerm(s, "p0:Bottomless Pool")
		if !ok {
			continue
		}
		seen++
		// Precondition: the permanent really is a battlefield Room, so an
		// empty name cannot pass because the object is absent or the wrong
		// type.
		if !oracleHasFold(room.Types, "Enchantment") || !oracleHasFold(room.Types, "Room") {
			t.Fatalf("precondition: snapshot %d (%s) types = %v, want Enchantment Room", i, s.Checkpoint, room.Types)
		}
		if room.Name != "" {
			t.Fatalf("snapshot %d (%s): both-locked Room name = %q, want the empty string", i, s.Checkpoint, room.Name)
		}
	}
	if seen == 0 {
		t.Fatalf("precondition: no snapshot lists the setup-placed Room p0:Bottomless Pool: %+v",
			res.Snapshots[len(res.Snapshots)-1].Permanents)
	}
}

func TestOracleRoomUnlockedDoorNames(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	run := func(t *testing.T, scenario string) OracleResult {
		t.Helper()
		res, err := RunOracleScenarioJSON(reg, []byte(scenario))
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Fails) != 0 {
			t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
		}
		return res
	}
	const ref = "p0:Bottomless Pool // Locker Room"

	// Back door: reuse the existing scenario constant rather than duplicate
	// it. Cast through the engine's room_alt offer, the Room shows the door
	// that was cast.
	back := run(t, roomAltFaceScenario)
	if room, ok := snapPerm(back.Snapshots[len(back.Snapshots)-1], ref); !ok {
		t.Fatalf("precondition: back-door Room not on the battlefield: %+v", back.Snapshots[len(back.Snapshots)-1].Permanents)
	} else if room.Name != "Locker Room" {
		t.Fatalf("back door shows %q, want Locker Room", room.Name)
	}

	// Front door.
	front := run(t, roomFrontDoorScenario)
	if room, ok := snapPerm(front.Snapshots[len(front.Snapshots)-1], ref); !ok {
		t.Fatalf("precondition: front-door Room not on the battlefield: %+v", front.Snapshots[len(front.Snapshots)-1].Permanents)
	} else if room.Name != "Bottomless Pool" {
		t.Fatalf("front door shows %q, want Bottomless Pool", room.Name)
	}

	// Both doors. The pre-unlock snapshots must show the front door alone, so
	// the combined name below cannot pass without the unlock actually
	// changing the door state.
	both := run(t, roomBothDoorsScenario)
	sawFront := false
	for _, s := range both.Snapshots {
		if room, ok := snapPerm(s, ref); ok && room.Name == "Bottomless Pool" {
			sawFront = true
		}
	}
	if !sawFront {
		t.Fatalf("precondition: no snapshot showed the front door alone before the unlock: %+v", both.Snapshots)
	}
	room, ok := snapPerm(both.Snapshots[len(both.Snapshots)-1], ref)
	if !ok {
		t.Fatalf("precondition: both-door Room not on the battlefield: %+v", both.Snapshots[len(both.Snapshots)-1].Permanents)
	}
	// The combined name is read from XMage's RoomCharacteristicsEffect
	// source, not yet confirmed by an XMage run.
	if room.Name != "Bottomless Pool // Locker Room" {
		t.Fatalf("both doors show %q, want %q", room.Name, "Bottomless Pool // Locker Room")
	}
}
