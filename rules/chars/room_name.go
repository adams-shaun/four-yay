package chars

import "github.com/adams-shaun/gorge/state"

// RoomName is the CR 709.5 name of a Room permanent: the name of each door
// that is currently unlocked, joined with " // " when both are, and the empty
// string when neither is. ok is false unless o carries a two-face card whose
// printed faces are both Rooms (cards.Face.IsRoom); every other object is not
// governed by this rule, and the caller falls back to the printed face name.
//
// XMage derives the same string in RoomCharacteristicsEffect: it starts from
// the split card's name ("Left // Right") and strips "Left // " when the left
// door is locked and " // Right" when the right is, leaving "" for a
// both-locked Room. Index 0 is the left (front) face, Forge's face 0.
//
// The door designations are read through state.Object.DoorUnlocked, the one
// home that folds the cast door (CastDoor), the alternate door (Unlocked) and
// the LockedDoors override together; this helper never re-reads those fields.
func RoomName(o *state.Object) (string, bool) {
	if o == nil || o.Card == nil || len(o.Card.Faces) != 2 {
		return "", false
	}
	faces := o.Card.Faces
	if faces[0] == nil || faces[1] == nil || !faces[0].IsRoom() || !faces[1].IsRoom() {
		return "", false
	}
	left, right := o.DoorUnlocked(0), o.DoorUnlocked(1)
	switch {
	case left && right:
		return faces[0].Name + " // " + faces[1].Name, true
	case left:
		return faces[0].Name, true
	case right:
		return faces[1].Name, true
	default:
		return "", true
	}
}
