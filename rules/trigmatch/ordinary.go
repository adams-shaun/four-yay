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
	if v := t.ParamStr(cards.PKValidCard); v != "" &&
		!e.MatchesObject(v, lki, source, you, SpecOpts{}) {
		// Forge matches ValidCard$ against the card AS THE CHANGE LEFT IT, not
		// the LKI: Zidane, Tantalus Thief's "an opponent gains control of a
		// permanent from you" is ValidCard$ Card.OppCtrl beside
		// ValidOriginalController$ You, and both only hold post-change (the
		// LKI carries the original controller, so the two filters contradict
		// there and the trigger could never fire). The LKI read stays first so
		// every ID-anchored carrier (Card.Self, IsRemembered) matches exactly
		// as before; only a card whose filter is controller-relative after the
		// move needs the live object.
		if live := e.Game().Obj(ev.Obj); live == nil || !e.MatchesObject(v, live, source, you, SpecOpts{}) {
			return false
		}
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
	return v == "" || effects.MatchesPlayerSpecFrom(e.Game(), v, ev.Player, e.ControllerOf(source), source)
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
