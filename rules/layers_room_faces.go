package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// staticRoomFaces includes the alternate face only when its door is unlocked
// on the battlefield. The cast face is still checked independently by the
// staticEffectsWalk face loop (it may have been relocked).
func staticRoomFaces(o *state.Object, f *cards.Face, onBattlefield bool) []*cards.Face {
	faces := []*cards.Face{f}
	if onBattlefield && o.RoomOtherDoorUnlocked() && isRoom(o) && len(o.Card.Faces) == 2 && int(o.FaceIdx) < len(o.Card.Faces) {
		faces = append(faces, o.Card.Faces[1-int(o.FaceIdx)])
	}
	return faces
}
