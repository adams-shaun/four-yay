package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func zoneChangeFilter(t *cards.Trigger) string {
	if t.ModeKind() == cards.TriggerChangesZoneAll {
		return t.ParamStr(cards.PKValidCards)
	}
	return t.ParamStr(cards.PKValidCard)
}

var (
	bounceProbes    = []string{"Unsummon"}
	exileZoneProbes = []string{"Swords to Plowshares", "Path to Exile"}
	reanimateProbes = []string{"Raise Dead", "Disentomb"}
)

// zoneTriggerRecipe builds p0-only causes for another card changing zones.
// The engine remains the authority on filters; this selects a legal candidate
// victim and spells whose destinations can satisfy the trigger.
func zoneTriggerRecipe(reg *cards.Registry, name string, t *cards.Trigger, sub string) ([]triggerCause, string) {
	filter := zoneChangeFilter(t)
	if sub == "trigger.zone-change-residue" {
		return nil, zoneChangeSkip(filter, t)
	}
	zone := state.ZBattlefield
	probes := bounceProbes
	if sub == "trigger.leaves-graveyard" {
		zone, probes = state.ZGraveyard, reanimateProbes
	} else {
		dest := strings.ToLower(t.ParamStr(cards.PKDestination))
		if strings.Contains(dest, "exile") {
			probes = exileZoneProbes
		}
	}
	victims := []string{bearsProbe}
	if bears, ok := reg.Lookup(bearsProbe); ok {
		fp := newFilterProbe(filter, zone)
		if fp.decided && !fp.accepts(bears) {
			victims = fp.victimProbes(reg, name, 4)
		}
	}
	if len(victims) == 0 {
		return nil, "zone-change filter " + filter
	}
	var out []triggerCause
	for _, victim := range victims {
		for _, probe := range probes {
			if probe == name {
				continue
			}
			c, ok := castCause(reg, name, probe, "p0:"+victim)
			if !ok {
				continue
			}
			if zone == state.ZGraveyard {
				c.graveyard = []string{victim}
			} else {
				c.battlefield = []string{victim}
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, zoneChangeSkip(filter, t)
	}
	return out, ""
}

func zoneChangeSkip(filter string, t *cards.Trigger) string {
	lower := strings.ToLower(filter)
	if strings.Contains(lower, "chosencardstrict") || strings.Contains(lower, "chosen card") || strings.Contains(lower, "chosen strict") {
		return "zone-change filter ChosenCardStrict"
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKOrigin)), "library") && strings.Contains(strings.ToLower(t.ParamStr(cards.PKDestination)), "graveyard") {
		return "library to graveyard"
	}
	if strings.Contains(strings.ToLower(t.ParamStr(cards.PKDestination)), "graveyard") {
		return "put into graveyard from anywhere"
	}
	return "zone-change filter " + filter
}
