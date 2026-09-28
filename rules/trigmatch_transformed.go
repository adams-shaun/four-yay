// Mode$ Transformed: "Whenever this permanent transforms into [this face]"
// (CR 701.26). Transform effects mark their FlipFace event with Text
// "Transformed"; other face changes, including alternate-face casting, do not
// satisfy this trigger. The object must be the source and remain on the
// battlefield, while ValidCard$/ValidPlayer$ are evaluated by the shared
// action-trigger matcher against the post-transform object.
package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) transformedMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.FlipFace || ev.Text != "Transformed" || ev.Obj == 0 || ev.Obj != source {
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
