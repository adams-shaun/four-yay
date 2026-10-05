package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Class keyword's level-up action changes a designation, not a counter.
func init() { Register("ClassLevelUp", effClassLevelUp) }

func effClassLevelUp(h Host, c *Ctx, sa *cards.SA) {
	o := h.Game().Obj(c.Source)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	h.Emit(events.Event{Kind: events.ClassLevelChange, Obj: o.ID, Amount: 1})
}
