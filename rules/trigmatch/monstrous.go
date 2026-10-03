package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Monstrosity (CR 701.31, task agent-20260919T190014Z): the mark is
// effPutCounter's `Monstrosity$` read, emitted as an events.AlterAttribute
// with Text "Monstrous" folded into state.Object.Monstrous; the offer gate is
// rules/legal.go's monstrosityGateOK; this file is the listener trigger.
// becomeMonstrousMatches implements Mode$ BecomeMonstrous ("When CARDNAME
// becomes monstrous, ..."): the mark event's Obj is the creature that just
// became monstrous (the trigger's own source for ValidCard$ Card.Self --
// every one of the 19 corpus carriers) and its Amount is the monstrosity
// COUNT, which the bodies read back as TriggerCount$Amount (Hydra
// Broodmaster's `SVar:MonstrosityX:TriggerCount$Amount`, Vitality Hunter's
// `SVar:MaxTgts:TriggerCount$Amount`) through the BecomeMonstrous referent
// bindings in rules/trigger_referents.go.
func becomeMonstrousMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.AlterAttribute || ev.Text != "Monstrous" {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	// The `T:Mode$ BecomeMonstrous` listener (19 corpus carrier files) is
	// registered as a non-API primitive so the coverage census reads the
	// carriers as playable -- the trig:Enlisted convention.
	registerTrigMatcher(becomeMonstrousMatches, "BecomeMonstrous")
}
