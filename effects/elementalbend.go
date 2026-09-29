package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() {
	Register("ElementalBend", effElementalBend)
	RegisterNonAPI("trig:ElementalBend")
}

func effElementalBend(h Host, c *Ctx, sa *cards.SA) {
	verb := sa.Params["Verb"]
	if verb != "water" && verb != "earth" && verb != "fire" && verb != "air" {
		return
	}
	h.Emit(events.Event{Kind: events.ElementalBend, Obj: c.Source, Player: c.Controller, Text: verb})
}
