package chars

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func roomSplitCard() *cards.Card {
	return &cards.Card{AlternateMode: "Split", Faces: []*cards.Face{
		{Name: "Left", Types: []string{"Enchantment", "Room"}},
		{Name: "Right", Types: []string{"Enchantment", "Room"}},
	}}
}

func TestRoomNameByDoorState(t *testing.T) {
	nonRoom := &cards.Card{AlternateMode: "Split", Faces: []*cards.Face{
		{Name: "Left", Types: []string{"Enchantment"}},
		{Name: "Right", Types: []string{"Sorcery"}},
	}}
	singleFace := &cards.Card{Faces: []*cards.Face{
		{Name: "Solo", Types: []string{"Enchantment", "Room"}},
	}}

	cases := []struct {
		name     string
		obj      state.Object
		wantL    bool
		wantR    bool
		wantName string
		wantOK   bool
	}{
		{"both locked", state.Object{Card: roomSplitCard(), FaceIdx: 0}, false, false, "", true},
		{"front cast, front unlocked", state.Object{Card: roomSplitCard(), FaceIdx: 0, CastDoor: true}, true, false, "Left", true},
		{"other door unlocked", state.Object{Card: roomSplitCard(), FaceIdx: 0, Unlocked: true}, false, true, "Right", true},
		{"both unlocked", state.Object{Card: roomSplitCard(), FaceIdx: 0, CastDoor: true, Unlocked: true}, true, true, "Left // Right", true},
		{"cast door re-locked", state.Object{Card: roomSplitCard(), FaceIdx: 0, CastDoor: true, Unlocked: true, LockedDoors: 1 << 0}, false, true, "Right", true},
		{"non-room split card", state.Object{Card: nonRoom, FaceIdx: 0, CastDoor: true}, false, false, "", false},
		// A single-face Room face IS a designated door (DoorUnlocked(0) is
		// true), yet the rule requires a two-face Room card, so ok is false.
		{"single-face card", state.Object{Card: singleFace, FaceIdx: 0, CastDoor: true}, true, false, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Precondition: the door designation the rule reads is what this
			// case claims. A zero-valued Object would otherwise make the name
			// assertion below vacuous.
			if got := tc.obj.DoorUnlocked(0); got != tc.wantL {
				t.Fatalf("precondition: DoorUnlocked(0)=%v, want %v", got, tc.wantL)
			}
			if got := tc.obj.DoorUnlocked(1); got != tc.wantR {
				t.Fatalf("precondition: DoorUnlocked(1)=%v, want %v", got, tc.wantR)
			}
			name, ok := RoomName(&tc.obj)
			if ok != tc.wantOK || name != tc.wantName {
				t.Fatalf("RoomName=%q,%v, want %q,%v", name, ok, tc.wantName, tc.wantOK)
			}
		})
	}
}
