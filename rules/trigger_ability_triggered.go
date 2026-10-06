package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// anyAbilityTriggeredWatcher reports whether a battlefield permanent carries a
// Mode$ AbilityTriggered line, the only reader of events.AbilityTriggered.
func anyAbilityTriggeredWatcher(g *state.Game) bool {
	for p := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, state.PlayerID(p)) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil {
				continue
			}
			for _, t := range o.Face().Triggers {
				if t.ModeKind() == cards.TriggerAbilityTriggered {
					return true
				}
			}
		}
	}
	return false
}

// emitAbilityTriggered records the provenance of the triggered ability pt just
// put on the stack (see events.AbilityTriggered): the causing trigger's mode
// and whether the source is the object whose event caused it. The causing
// object is read from the capture the queue stored (TriggerCard, else the
// remembered event objects -- the attacker of an Attacks line).
func emitAbilityTriggered(g *state.Game, triggerOf func(pendingTrigger) (cards.Trigger, bool),
	emit func(events.Event) events.Event, pt pendingTrigger) {
	t, ok := triggerOf(pt)
	if !ok || !anyAbilityTriggeredWatcher(g) {
		return
	}
	own := pt.Ctx.TriggerContext.TriggerCard == pt.Source
	for _, tgt := range pt.Ctx.Remembered {
		if !tgt.IsPlayer && tgt.Obj == pt.Source {
			own = true
		}
	}
	var amount int32
	if own {
		amount = 1
	}
	emit(events.Event{Kind: events.AbilityTriggered, Player: pt.Controller,
		Obj: pt.Source, Amount: amount, Counter: t.Mode})
}
