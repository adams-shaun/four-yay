package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// tapeHost is fakeHost plus the ask seam (AskCount/TapeAnswer), so an
// effects-package test can serve a real answer to a mid-resolution KChoose
// without an engine. Only the lock/unlock choice is served: its first option
// is Kind "unlock" (askUnlockDoorHalf's fixed option order). Any other
// decision falls through to the deterministic stand-in, exactly as
// fakeHost's Ask does.
type tapeHost struct {
	*fakeHost
	// choiceIdx is the option index TapeAnswer returns for the lock/unlock
	// choice; a negative value serves no answer (the R-9 stand-in applies).
	choiceIdx int
	// served records every lock/unlock decision the seam was offered.
	served []*decision.Decision
}

func (h *tapeHost) AskCount() uint64 { return uint64(h.askCount) }

func (h *tapeHost) TapeAnswer(d *decision.Decision) (decision.Intent, bool) {
	if d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "unlock" {
		return decision.Intent{}, false
	}
	h.served = append(h.served, d)
	if h.choiceIdx < 0 {
		return decision.Intent{}, false
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{h.choiceIdx}}, true
}

func newTapeHost(t *testing.T, choiceIdx int) *tapeHost {
	t.Helper()
	return &tapeHost{fakeHost: newHost(t, 2), choiceIdx: choiceIdx}
}

// TestUnlockDoorMinZeroElectedNoTargetDoesNothing pins the MAJOR-1 regression:
// Ghostly Keybearer is `Mode$ Unlock | ValidTgts$ Room.YouCtrl | TargetMin$ 0
// | TargetMax$ 1`. When the announcement ask OFFERED that targeting and the
// chooser elected ZERO targets, Ctx.Targets is empty but Ctx.TargetsOffered is
// true. The effect must NOT fall through to the Choices$/all-controlled pool
// and unlock a Room the player did not choose. Precondition: a controlled
// locked Room exists that the wide fallback WOULD reach, so the assertion can
// fail if the pool decides from `len(c.Targets) != 0`.
func TestUnlockDoorMinZeroElectedNoTargetDoesNothing(t *testing.T) {
	h := newHost(t, 2)
	room := testRoomDoorCard(t, "Elected Room", "Elected Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the controlled Room must have a locked door for the wide fallback to reach")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":      "Unlock",
		"ValidTgts": "Room.YouCtrl",
		"TargetMin": "0",
		"TargetMax": "1",
	}}
	// The announcement ask covered exactly this SA and the chooser elected
	// zero: TargetsOffered set, Targets empty.
	Resolve(h, &Ctx{Source: id, Controller: 0, TargetsOffered: true, OfferedSA: sa}, sa)
	if h.g.Obj(id).Unlocked {
		t.Fatal("a Min-0 elected-zero unlocked a Room the player did not choose")
	}
	if got := doorUnlockEvents(h); len(got) != 0 {
		t.Fatalf("DoorUnlock events = %+v, want none", got)
	}
}

// TestUnlockDoorLockOrUnlockLockAnswerIsLoud pins the MAJOR-2 fix: the seat
// answers the posed lock/unlock choice with the LOCK half. The one-designation
// Room model cannot lock a half, so the effect must report loudly and change
// nothing -- never silently take the unlock half. Precondition: the target
// Room has a locked door, so a silent unlock WOULD be observable.
func TestUnlockDoorLockOrUnlockLockAnswerIsLoud(t *testing.T) {
	h := newTapeHost(t, 1) // option 1 = "Lock a door"
	room := testRoomDoorCard(t, "Lock Choice Room", "Lock Choice Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must have a locked door")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":      "LockOrUnlock",
		"ValidTgts": "Room.YouCtrl",
	}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, sa)

	if len(h.served) != 1 || h.served[0].Options[0].Kind != "unlock" || h.served[0].Options[1].Kind != "lock" {
		t.Fatalf("precondition: the lock/unlock choice was not posed as unlock/lock: %+v", h.served)
	}
	if h.g.Obj(id).Unlocked {
		t.Fatal("the seat elected to LOCK a door, but the effect unlocked it")
	}
	if got := doorUnlockEvents(h.fakeHost); len(got) != 0 {
		t.Fatalf("DoorUnlock events = %+v, want none for the unmodelled lock half", got)
	}
	notes := noteEvents(h.fakeHost)
	if len(notes) != 1 || notes[0].Text == "" {
		t.Fatalf("electing the lock half must emit one loud Note, got %+v", h.log)
	}
}

// TestUnlockDoorLockOrUnlockUnlockAnswerUnlocks is the productive half of the
// same choice: the seat elects UNLOCK and the locked target is unlocked.
func TestUnlockDoorLockOrUnlockUnlockAnswerUnlocks(t *testing.T) {
	h := newTapeHost(t, 0) // option 0 = "Unlock a door"
	room := testRoomDoorCard(t, "Unlock Choice Room", "Unlock Choice Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must have a locked door")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":      "LockOrUnlock",
		"ValidTgts": "Room.YouCtrl",
	}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, sa)

	if len(h.served) != 1 {
		t.Fatalf("precondition: the lock/unlock choice was not posed: %+v", h.served)
	}
	if !h.g.Obj(id).Unlocked {
		t.Fatal("the seat elected UNLOCK, but the target was not unlocked")
	}
	if got := doorUnlockEvents(h.fakeHost); len(got) != 1 {
		t.Fatalf("DoorUnlock events = %d, want 1", len(got))
	}
	if notes := noteEvents(h.fakeHost); len(notes) != 0 {
		t.Fatalf("the unlock half must not report the unimplemented lock half: %+v", notes)
	}
}

// TestUnlockDoorLockOrUnlockOnFullyUnlockedAsksBeforeActing is the
// fully-unlocked case the finding named: a fully unlocked Room has an
// unlocked alternate door that could be LOCKED, so the choice is reachable and
// must be posed. With no answer served, the deterministic stand-in is the
// unlock half, which finds no locked door and changes nothing.
func TestUnlockDoorLockOrUnlockOnFullyUnlockedAsksBeforeActing(t *testing.T) {
	h := newTapeHost(t, -1) // no answer -> R-9 stand-in (unlock)
	room := testRoomDoorCard(t, "Full Choice Room", "Full Choice Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	o.Unlocked = true
	if roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must already be fully unlocked")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":      "LockOrUnlock",
		"ValidTgts": "Room.YouCtrl",
	}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, sa)

	if len(h.served) != 1 {
		t.Fatalf("precondition: the reachable lock/unlock choice was not posed: %+v", h.served)
	}
	if !h.g.Obj(id).Unlocked {
		t.Fatal("the fully unlocked Room must stay fully unlocked on the unlock stand-in")
	}
	if got := doorUnlockEvents(h.fakeHost); len(got) != 0 {
		t.Fatalf("DoorUnlock events = %+v, want none (no locked door to unlock)", got)
	}
}

// TestUnlockDoorUnlockModeDoesNotPoseLockChoice guards against the choice
// leaking into the plain Mode$ Unlock path: Ghostly Keybearer/Dancers never
// offer a lock half, so no lock/unlock decision may be posed there.
func TestUnlockDoorUnlockModeDoesNotPoseLockChoice(t *testing.T) {
	h := newTapeHost(t, 1)
	room := testRoomDoorCard(t, "Plain Room", "Plain Chamber")
	id := h.g.AddObject(room, 0).ID
	o := h.g.Obj(id)
	o.Zone = state.ZBattlefield
	if !roomHasLockedDoor(o, 0) {
		t.Fatal("precondition: the target Room must have a locked door")
	}
	sa := &cards.SA{Kind: "DB", API: "UnlockDoor", Params: map[string]string{
		"Mode":      "Unlock",
		"ValidTgts": "Room.YouCtrl",
	}}
	Resolve(h, &Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}, sa)

	if len(h.served) != 0 {
		t.Fatalf("Mode$ Unlock must not pose the lock/unlock choice: %+v", h.served)
	}
	if !h.g.Obj(id).Unlocked {
		t.Fatal("Mode$ Unlock on a locked target did not unlock it")
	}
}
