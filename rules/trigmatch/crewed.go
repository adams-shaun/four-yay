package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CrewedMatches implements Mode$ Crewed ("Whenever this creature crews a
// Vehicle ...", CR 702.122; Canyon Vaulter, Reckless Velocitaur, Veteran
// Motorist and the mode's 7 corpus carriers). It reads the events.Crew marker
// rules/pay's Crew cost records: one event per crewing creature, Obj the
// CREWING creature and IDs[0] the Vehicle it crewed. Forge's ValidCrew$
// names the crewer (Card.Self on every carrier), so it is filtered against
// ev.Obj -- NOT ev.IDs[0] -- mirroring EnlistedMatches' ValidEnlisted$ vs
// ev.IDs[0] split.
func CrewedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Crew {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCrew); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

// SaddledMatches implements Mode$ Saddled ("Whenever this creature saddles a
// Mount ...", CR 702.171; Canyon Vaulter, Reckless Velocitaur). It is the
// exact twin of CrewedMatches over the events.Saddle marker rules/pay's
// Saddle cost records: Obj the SADDLING creature, IDs[0] the Mount. Forge's
// ValidCrew$ names the saddler, so it filters ev.Obj, exactly as the Crewed
// half does.
func SaddledMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.Saddle {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidCrew); v != "" && !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(CrewedMatches, "Crewed")
	registerTrigMatcher(SaddledMatches, "Saddled")
}
