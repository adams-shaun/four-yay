package rules

import (
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// meleeRemembered captures one player reference per distinct opponent attacked
// in the WHOLE declaration. The Pump body reads this capture (cards.MeleePumpCount)
// snapshot after the ability is pushed and later resolves, even if an attacker
// or defender leaves combat in the meantime. A synthetic single-defender event
// has no declaration scratch and captures just that event's defending seat.
func (e *Engine) meleeRemembered(ev events.Event) []state.Target {
	defenders := e.declaredDefenders
	if len(defenders) == 0 {
		defenders = []state.PlayerID{ev.Player}
	}
	out := make([]state.Target, 0, len(defenders))
	for _, p := range defenders {
		out = append(out, state.Target{Player: p, IsPlayer: true})
	}
	return out
}

// checkGrantedMeleeTriggers synthesizes only the keyword instances not already
// represented by a printed Melee trigger. Derived preserves duplicates: two
// Titanias grant two instances to another creature, and a printed Melee plus
// a layer-6 grant likewise triggers twice (CR 702.121a). Every instance gets
// its own stack object with the same declaration-wide opponent snapshot.
func (e *Engine) checkGrantedMeleeTriggers(observer *Engine, id state.ObjID, o *state.Object, f *cards.Face, ev events.Event, objLKI *state.Object) {
	if ev.Kind != events.DeclareAttackers || !slices.Contains(ev.IDs, id) {
		return
	}
	instances := 0
	for _, kw := range e.Derived(id).Keywords {
		if cards.KeywordHead(kw) == "Melee" {
			instances++
		}
	}
	for _, t := range f.Triggers {
		if t.Mode == "Attacks" && t.ParamStr(cards.PKKeyword) == "Melee" {
			instances-- // this printed marker is queued by the ordinary trigger walk
		}
	}
	if instances <= 0 {
		return
	}
	t := cards.Trigger{Mode: "Attacks", Params: map[string]string{"Mode": "Attacks", "ValidCard": "Card.Self"}}
	if !observer.triggerMatches(t, id, ev, objLKI) {
		return
	}
	for i := 0; i < instances; i++ {
		key := triggerKey{Source: id, Idx: -1}
		if e.triggerFireCount == nil {
			e.triggerFireCount = map[triggerKey]int32{}
		}
		if e.triggerFireCount[key] >= maxTriggerFires {
			return
		}
		e.triggerFireCount[key]++
		r := e.meleeRemembered(ev)
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source: id, Controller: o.Controller, Idx: -1, Melee: true,
			cause: causeOf(boardOf(observer), t, id, ev, 0),
			Ctx: effects.NewCtx(id, o.Controller, effects.CtxInit{Remembered: r,
				Captured: r, LKI: objLKI, TriggerContext: observer.triggerReferents(t, id, ev, objLKI)}),
		})
	}
}
