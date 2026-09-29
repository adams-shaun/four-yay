package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func (e *Engine) elementalBendMatches(t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.ElementalBend {
		return false
	}
	ctrl := e.controllerOf(source)
	if v := t.Params["ValidCard"]; v != "" && ev.Obj != 0 && !e.matchesSpec(v, ev.Obj, e.specCtx(source, ctrl)) {
		return false
	}
	if v := t.Params["ValidPlayer"]; v != "" && !effects.MatchesPlayerSpec(e.G, v, ev.Player, ctrl) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher((*Engine).elementalBendMatches, "ElementalBend")
	effects.RegisterNonAPI("trig:ElementalBend")
}
