package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/state"
)

var (
	bounceProbes    = []string{"Unsummon"}
	exileZoneProbes = []string{"Swords to Plowshares", "Path to Exile"}
	reanimateProbes = []string{"Raise Dead", "Disentomb"}
	// tokenProbes make ONE token: a ChangesZoneAll "one or more enter"
	// trigger fires once per token in gorge, so a two-token maker queues two
	// triggers behind a trigger_order ask and none reaches the stack.
	tokenProbes = []string{"Sprout"}
)

// zoneProbes lists the spells whose destination satisfies a trigger's
// Destination$: Any is bounced, exiled or destroyed; a list takes the probes
// of each member it names. A destination no probe reaches yields nil.
func zoneProbes(dest string) []string {
	var out []string
	for _, d := range strings.Split(strings.ToLower(dest), ",") {
		switch strings.TrimSpace(d) {
		case "", "any":
			out = append(out, bounceProbes...)
			out = append(out, exileZoneProbes...)
			out = append(out, destroyProbes...)
		case "hand":
			out = append(out, bounceProbes...)
		case "exile":
			out = append(out, exileZoneProbes...)
		case "graveyard":
			out = append(out, destroyProbes...)
		}
	}
	return out
}

// zoneTriggerRecipe builds p0-only causes for another card changing zones.
// The engine remains the authority on filters; this selects a legal candidate
// victim and spells whose destinations can satisfy the trigger.
func zoneTriggerRecipe(reg *cards.Registry, name string, t *cards.Trigger, sub string) ([]triggerCause, string) {
	filter := levelb.ZoneChangeFilter(t)
	if sub == "trigger.zone-change-residue" {
		return nil, zoneChangeSkip(filter, t)
	}
	dest := t.ParamStr(cards.PKDestination)
	zone := state.ZBattlefield
	probes := zoneProbes(dest)
	if sub == "trigger.leaves-graveyard" {
		// Only a return to hand takes a card out of a graveyard for these
		// probes; Any admits it.
		zone, probes = state.ZGraveyard, nil
		if strings.EqualFold(dest, "Any") || strings.Contains(","+strings.ToLower(dest)+",", ",hand,") {
			probes = reanimateProbes
		}
	}
	if len(probes) == 0 {
		return nil, "zone-change destination " + dest
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
