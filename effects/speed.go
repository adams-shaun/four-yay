package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("SpeedIncrease", effSpeedIncrease) }

// effSpeedIncrease resolves CR 702.179d's source-less inherent trigger.
// Its intervening if (speed < 4) is checked again on resolution.
func effSpeedIncrease(h Host, c *Ctx, _ *cards.SA) {
	p := c.Controller
	g := h.Game()
	if int(p) >= len(g.Players) || g.Players[p].Lost || g.Players[p].Speed < 1 || g.Players[p].Speed >= 4 {
		return
	}
	h.Emit(events.Event{Kind: events.SpeedChange, Player: p, Amount: 1, Text: "speed"})
}
