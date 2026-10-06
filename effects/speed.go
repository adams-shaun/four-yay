package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("SpeedIncrease", effSpeedIncrease) }

// maxSpeed is CR 702.179b/e's max-speed threshold (4), the same value
// rules/speed.go's maxSpeed holds. A player's speed (state.Player.Speed, an
// event-folded latch written only by events.SpeedChange) is max speed
// exactly at this value, which is what the max-speed player property reads
// (HasPropertyMaxSpeed) and what Condition$ MaxSpeed gates elsewhere.
const maxSpeed = 4

// effSpeedIncrease resolves CR 702.179d's source-less inherent trigger.
// Its intervening if (speed < 4) is checked again on resolution.
func effSpeedIncrease(h Host, c *Ctx, _ *cards.SA) {
	p := c.Controller
	g := h.Game()
	if int(p) >= len(g.Players) || g.Players[p].Lost || g.Players[p].Speed < 1 || g.Players[p].Speed >= maxSpeed {
		return
	}
	h.Emit(events.Event{Kind: events.SpeedChange, Player: p, Amount: 1, Text: "speed"})
}
