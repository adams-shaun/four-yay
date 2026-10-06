package trigmatch

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// abilityTriggeredMatches implements Mode$ AbilityTriggered ("whenever X
// causes a triggered ability of Y to trigger"; Firebender Ascension, Aboleth
// Spawn, Historian's Boon, Strict Proctor). The event is the
// events.AbilityTriggered marker: Obj the ability's source, Counter the
// CAUSING trigger's mode (ValidMode$), Amount 1 for the source's own ability
// (TriggeredOwnAbility$ True). ValidDestination$ and ValidSpellAbility$
// (three of the four carriers) are not modelled by the marker, so a line
// carrying them fails closed rather than firing wide.
func abilityTriggeredMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.AbilityTriggered {
		return false
	}
	if t.ParamStr(cards.PKValidDestination) != "" || t.ParamStr(cards.PKValidSpellAbility) != "" {
		return false
	}
	if v := t.ParamStr(cards.PKValidMode); v != "" {
		found := false
		for m := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(m), ev.Counter) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if ParamTrue(t, cards.PKTriggeredOwnAbility) && ev.Amount != 1 {
		return false
	}
	if v := t.ParamStr(cards.PKValidSource); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, e.ControllerOf(source), SpecOpts{}) {
		return false
	}
	return true
}

func init() { registerTrigMatcher(abilityTriggeredMatches, "AbilityTriggered") }
