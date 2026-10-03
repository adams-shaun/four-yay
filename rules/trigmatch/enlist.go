package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// EnlistedMatches implements Mode$ Enlisted ("Whenever CARDNAME enlists a
// creature"): the Enlist event's Obj is the ATTACKING creature that enlisted
// (ValidCard$ Card.Self names the trigger's own source), and its IDs[0] the
// creature it tapped, which ValidEnlisted$ filters (Goblin Morale Sergeant's
// Creature.!token).
func EnlistedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Enlist || len(ev.IDs) == 0 {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	if v := t.Params["ValidEnlisted"]; v != "" && !e.MatchesSpec(v, ev.IDs[0], source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(EnlistedMatches, "Enlisted")
}
