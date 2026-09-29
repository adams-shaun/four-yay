// Mode$ Transformed: "Whenever this permanent transforms into [this face]"
// (CR 701.26). SetState Mode$ Transform on the battlefield marks its
// FlipFace event with Text "Transformed". Flip, alternate-face casting, and
// ChangeZone Transformed$ True (which flips before entry) do not satisfy this
// trigger. The transformed object must remain on the battlefield and be the
// event's own object; ValidCard$/ValidPlayer$ scope which transformed object
// fires THIS trigger -- Card.Self for a self-transform body (Brigid), a
// type/control predicate for a watcher (Cult of the Waxing Moon,
// Corruption of Towashi, Norn's Inquisitor) or an attachment predicate
// (Neglected Heirloom's Creature.EquippedBy).
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) transformedMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.FlipFace || ev.Text != "Transformed" || ev.Obj == 0 {
		return false
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone != state.ZBattlefield || o.Card == nil || len(o.Card.Faces) < 2 {
		return false
	}
	return e.eventCardAndPlayerMatch(t, source, ev.Obj, o.Controller)
}

func init() {
	registerTrigMatcher((*Engine).transformedMatches, "Transformed")
}
