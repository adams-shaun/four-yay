package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
)

func init() { Register("ChangeSpeed", effChangeSpeed) }

// effChangeSpeed changes each named player's speed by one. SpeedChange's
// replay fold enforces the zero-to-four bounds. Increasing zero starts engines;
// decreasing one leaves speed at one (CR 702.179).
func effChangeSpeed(h Host, c *Ctx, sa *cards.SA) {
	delta := int32(0)
	mode := sa.ParamStr(cards.PKMode)
	if mode == "Increase" {
		delta = 1
	} else if mode == "Decrease" {
		delta = -1
	} else {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unimplemented ChangeSpeed mode: " + mode})
		return
	}
	selector := DefinedRefOf(sa).Raw
	targets, ok := knownDefinedTargets(h, c, selector)
	if !ok {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "unimplemented ChangeSpeed selector: " + selector})
		return
	}
	seen := make(map[int]bool)
	for _, target := range targets {
		if !target.IsPlayer || seen[int(target.Player)] || int(target.Player) >= len(h.Game().Players) {
			continue
		}
		seen[int(target.Player)] = true
		p := h.Game().Players[target.Player]
		if p.Lost || (delta > 0 && p.Speed >= 4) || (delta < 0 && p.Speed <= 1) {
			continue
		}
		h.Emit(events.Event{Kind: events.SpeedChange, Player: target.Player, Amount: delta, Text: "speed"})
	}
}
