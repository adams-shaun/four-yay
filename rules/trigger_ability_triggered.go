package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/trigmatch"
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

// triggerCause is a queued trigger's provenance for Mode$ AbilityTriggered
// (events.AbilityTriggered): the CAUSING trigger line's mode and whether the
// ability's source is itself the object whose event caused it. It is stamped
// where the line and the event are both in hand (the queue sites), never
// re-derived at push time, because the push arms for granted, gained, merged
// and synthesized keyword abilities have no printed face index to recover the
// line from. set is false when no AbilityTriggered watcher was on the
// battlefield at queue time, so such games emit no marker.
type triggerCause struct {
	mode     string
	own, set bool
}

// causeOf stamps t's provenance for ev. An attack-declaration line reads the
// attackers its own filter selected (trigmatch.AttackCausedBySource); every
// other event's causing object is the TriggerCard role (triggerCard).
func causeOf(b trigmatch.Board, t cards.Trigger, source state.ObjID, ev events.Event,
	triggerCard state.ObjID) triggerCause {
	if !anyAbilityTriggeredWatcher(b.Game()) {
		return triggerCause{}
	}
	own, ok := trigmatch.AttackCausedBySource(b, t, source, ev)
	if !ok {
		own = triggerCard == source
	}
	return triggerCause{mode: t.Mode, own: own, set: true}
}

// emitAbilityTriggered records the provenance of the triggered ability pt just
// put on the stack (see events.AbilityTriggered). It runs deferred from
// pushTrigger so every push arm is covered; stackLen is the stack height on
// entry, so a push that minted nothing records nothing.
func emitAbilityTriggered(g *state.Game, emit func(events.Event) events.Event, pt pendingTrigger, stackLen int) {
	if !pt.cause.set || len(g.Stack) <= stackLen {
		return
	}
	var amount int32
	if pt.cause.own {
		amount = 1
	}
	emit(events.Event{Kind: events.AbilityTriggered, Player: pt.Controller,
		Obj: pt.Source, Amount: amount, Counter: pt.cause.mode})
}
