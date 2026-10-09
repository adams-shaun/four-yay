package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Level-B sub-families of the Room unlock triggers (CR 709.5, 702.xx Eerie).
// Both are caused by a Room entering or being unlocked, never by setup
// placement: a Room placed on the battlefield has no unlocked door in either
// engine, so the cause is casting a door (and, for FullyUnlock, paying to
// unlock the other). Exported so the recipes in compliance/oraclegen/templates
// share one spelling with this classifier.
const (
	UnlockDoorSub  = "trigger.unlock-door"
	FullyUnlockSub = "trigger.fully-unlock"
)

// Forge's Mode$ words for the two Room triggers. UnlockDoor has no cards
// vocabulary code, so it is matched by name here; FullyUnlock has one.
const unlockDoorMode = "UnlockDoor"

// classifyRoomTrigger names the sub-family of the two Room trigger shapes:
// "when you unlock this door" (the card's own door, its controller) and the
// Eerie "whenever you fully unlock a Room". Any other UnlockDoor or
// FullyUnlock filter stays a named gap.
func classifyRoomTrigger(t *cards.Trigger) (sub string, ok bool) {
	if !strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
		return "", false
	}
	switch {
	case strings.EqualFold(t.Mode, unlockDoorMode):
		if namesSelf(t.ParamStr(cards.PKValidCard)) {
			return UnlockDoorSub, true
		}
	case t.ModeKind() == cards.TriggerFullyUnlock:
		if strings.EqualFold(t.ParamStr(cards.PKValidCard), "Card.Room") {
			return FullyUnlockSub, true
		}
	}
	return "", false
}

// IsRoomCard reports whether c is a Room: two faces, both Enchantment Rooms
// (a Forge AlternateMode:Split Room). Either door can be cast from hand, which
// is how the generator reaches the second door's requirements.
func IsRoomCard(c *cards.Card) bool {
	if c == nil || len(c.Faces) != 2 {
		return false
	}
	for _, f := range c.Faces {
		if !f.IsEnchantment() || !f.IsRoom() {
			return false
		}
	}
	return true
}

// roomDoorServable reports whether a trigger on a Room's second door is
// served: the template casts that door from hand instead of placing it, so
// the face-1 gap lifts for Rooms. Only trigger requirements qualify; an
// activated ability, static or combat requirement on a Room's second door
// stays a face gap because no template casts the door for them.
func roomDoorServable(c *cards.Card) bool {
	return IsRoomCard(c)
}

// RoomStaticServable reports whether a static on a Room's second door is
// served by casting that door: only the sub-families whose builders either
// already cast the card (panharmonicon's cast base) or are being given the
// room-door cast base qualify. Any other sub keeps the face gap, because its
// builder would place the door on the battlefield by setup, where neither
// engine unlocks it.
func RoomStaticServable(c *cards.Card, sub string) bool {
	if !IsRoomCard(c) {
		return false
	}
	switch sub {
	case "static.continuous", "static.panharmonicon", "static.untap-other-player":
		return true
	}
	return false
}
