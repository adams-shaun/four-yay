package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func keywordActionMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, kind events.Kind) bool {
	if ev.Kind != kind {
		return false
	}
	if v := t.ParamStr(cards.PKValidPlayer); v != "" {
		return effects.MatchesPlayerSpec(e.Game(), v, ev.Player, e.ControllerOf(source))
	}
	return true
}

func forageMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	return keywordActionMatches(e, t, source, ev, events.ForageAction)
}
func manifestDreadMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	return keywordActionMatches(e, t, source, ev, events.ManifestDreadAction)
}
func collectEvidenceMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	return keywordActionMatches(e, t, source, ev, events.CollectEvidenceAction)
}

func init() {
	registerTrigMatcher(forageMatches, "Forage")
	registerTrigMatcher(manifestDreadMatches, "ManifestDread")
	registerTrigMatcher(collectEvidenceMatches, "CollectEvidence")
}
