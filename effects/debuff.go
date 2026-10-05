package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("Debuff", effDebuff) }

// effDebuff registers layer-6 keyword loss on the resolved permanents. The
// ordinary resolver remains responsible for the SubAbility$ tail.
func effDebuff(h Host, c *Ctx, sa *cards.SA) {
	p := DebuffOf(sa)
	if len(p.Keywords) == 0 {
		h.Emit(events.Event{Kind: events.Note, Obj: c.Source, Text: "Debuff has no Keywords$"})
		return
	}
	permanent, untilEOT := durationTiming(p.Duration)
	if IsNextTurnDuration(p.Duration) {
		untilEOT = false
	}
	for _, t := range Defined(h, c, sa) {
		if t.IsPlayer {
			continue
		}
		o := h.Game().Obj(t.Obj)
		if o == nil || o.Zone != state.ZBattlefield {
			continue
		}
		h.AddContinuous(state.ContinuousEffect{
			Source: o.ID, Affects: "Card.Self", Controller: c.Controller,
			Layer: state.LAbilities, RemoveKeywords: p.Keywords,
			Duration: p.Duration, Permanent: permanent, UntilEOT: untilEOT,
		})
	}
}
