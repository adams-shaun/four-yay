package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func exiledMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	return ev.Kind == events.MoveZone && ev.From == state.ZBattlefield && ev.To == state.ZExile &&
		ZoneChangeMatches(e, t, source, ev, lki)
}

func changesControllerMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.ControlChange || lki == nil || lki.Controller == ev.Player {
		return false
	}
	you := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCard); v != "" && !e.MatchesObject(v, lki, source, you, SpecOpts{}) {
		return false
	}
	if v := t.ParamStr(cards.PKValidOriginalController); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, lki.Controller, you) {
		return false
	}
	if v := t.ParamStr(cards.PKValidNewController); v != "" && !effects.MatchesPlayerSpec(e.Game(), v, ev.Player, you) {
		return false
	}
	return true
}

func losesGameMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.PlayerLost {
		return false
	}
	v := t.ParamStr(cards.PKValidPlayer)
	return v == "" || effects.MatchesPlayerSpec(e.Game(), v, ev.Player, e.ControllerOf(source))
}

func turnBeginMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.TurnChange {
		return false
	}
	v := t.ParamStr(cards.PKValidPlayer)
	return v == "" || effects.MatchesPlayerSpec(e.Game(), v, ev.Player, e.ControllerOf(source))
}

func init() {
	registerTrigMatcher(exiledMatches, "Exiled")
	registerTrigMatcher(changesControllerMatches, "ChangesController")
	registerTrigMatcher(losesGameMatches, "LosesGame")
	registerTrigMatcher(turnBeginMatches, "TurnBegin")
}
