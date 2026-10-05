package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("ChangeSpeed", effChangeSpeed) }

// effChangeSpeed changes each named player's speed by one. SpeedChange's
// replay fold enforces the zero-to-four bounds; zero cannot be increased here
// because starting engines is a separate action, not a speed increase.
func effChangeSpeed(h Host, c *Ctx, sa *cards.SA) {
	delta := int32(0)
	switch sa.ParamStr(cards.PKMode) {
	case "Increase":
		delta = 1
	case "Decrease":
		delta = -1
	default:
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unimplemented ChangeSpeed mode: " + sa.ParamStr(cards.PKMode)})
		return
	}
	targets, ok := knownDefinedTargets(h, c, sa.ParamStr(cards.PKDefined))
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unimplemented ChangeSpeed selector: " + sa.ParamStr(cards.PKDefined)})
		return
	}
	seen := make(map[int]bool)
	for _, target := range targets {
		if !target.IsPlayer || seen[int(target.Player)] || int(target.Player) >= len(h.Game().Players) {
			continue
		}
		seen[int(target.Player)] = true
		p := h.Game().Players[target.Player]
		if p.Lost || (delta > 0 && (p.Speed == 0 || p.Speed >= 4)) || (delta < 0 && p.Speed == 0) {
			continue
		}
		h.Emit(events.Event{Kind: events.SpeedChange, Player: target.Player, Amount: delta, Text: "speed"})
	}
}
