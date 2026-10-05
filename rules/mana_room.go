package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// roomFaceManaAbilities keeps the payment walk's printed mana abilities in
// sync with the locked-face designation. Under-cards in a pile retain their
// own abilities; only the top Room face can be locked.
func roomFaceManaAbilities(o *state.Object, face *cards.Face, top bool) []*cards.SA {
	if top && o.Zone == state.ZBattlefield && face.IsRoom() && !o.DoorUnlocked(int(o.FaceIdx)) {
		return nil
	}
	return face.ManaAbilities()
}
