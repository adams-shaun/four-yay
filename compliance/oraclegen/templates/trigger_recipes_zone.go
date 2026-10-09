package templates

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/state"
)

var (
	bounceProbes    = []string{"Unsummon"}
	exileZoneProbes = []string{"Swords to Plowshares", "Path to Exile"}
	reanimateProbes = []string{"Raise Dead", "Disentomb", "Breath of Life"}
	// artifactProbes and enchantmentProbes destroy a noncreature permanent.
	artifactProbes    = []string{"Shatter"}
	enchantmentProbes = []string{"Disenchant", "Naturalize"}
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
		if causes, why, ok := residueCauses(reg, name, t, filter); ok {
			return causes, why
		}
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
	withCounter := false
	if bears, ok := reg.Lookup(bearsProbe); ok {
		fp := newFilterProbe(filter, zone)
		if fp.decided && !fp.accepts(bears) {
			// One +1/+1 counter serves modified/HasCounters/counters_GE1
			// without interpreting those predicates here. The same matcher
			// checks the fixture we will actually emit.
			fp.p1p1 = zone == state.ZBattlefield
			if fp.accepts(bears) {
				withCounter = fp.p1p1
			} else {
				fp.p1p1 = false
				victims = fp.victimProbes(reg, name, 4)
				if len(victims) == 0 && zone == state.ZBattlefield {
					fp.p1p1 = true
					victims = fp.victimProbes(reg, name, 4)
					withCounter = true
				}
			}
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
				if withCounter {
					c.counters = map[string]map[string]int{victim: {"P1P1": 1}}
				}
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, zoneChangeSkip(filter, t)
	}
	return out, ""
}

// zoneETBHistorySkip names enter-together filters whose every alternative
// requires graveyard/exile provenance. Casting from hand or making a token
// cannot supply it. An OR branch without that constraint stays probeable.
func zoneETBHistorySkip(t *cards.Trigger, filter string) string {
	if origin := t.ParamStr(cards.PKOrigin); strings.EqualFold(origin, "Graveyard") || strings.EqualFold(origin, "Exile") {
		return "etb origin " + origin
	}
	for _, alt := range strings.Split(strings.ToLower(filter), ",") {
		if !strings.Contains(alt, "wascastfromgraveyard") && !strings.Contains(alt, "thisturnenteredfrom_graveyard") &&
			!strings.Contains(alt, "wascastfromexile") && !strings.Contains(alt, "thisturnenteredfrom_exile") {
			return ""
		}
	}
	return "etb filter zone history (" + filter + ")"
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

var powerGEGate = regexp.MustCompile(`(?i)\bpowerGE(\d+)`)

// ltbSelfSub is the sub-family of "when this leaves the battlefield".
const ltbSelfSub = "trigger.ltb-self"

// ltbSelfRecipe removes the card itself from the battlefield on turn 1: a
// creature is bounced, exiled or destroyed by the probe the trigger's
// Destination$ admits, an artifact or enchantment is destroyed by the
// matching removal spell (their destination is the graveyard). The engine
// remains the authority: a probe the card's own gate refuses is simply tried
// past. A "powerGE<N>" gate on a card printed lower gets the counters that
// reach N.
func ltbSelfRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) ([]triggerCause, string) {
	dest := t.ParamStr(cards.PKDestination)
	probes := zoneProbes(dest)
	if !f.IsCreature() {
		probes = nil
		if zoneProbesReachGraveyard(dest) {
			if f.IsArtifact() {
				probes = append(probes, artifactProbes...)
			}
			if f.IsEnchantment() {
				probes = append(probes, enchantmentProbes...)
			}
		}
	}
	if len(probes) == 0 {
		return nil, "ltb-self cause for " + strings.Join(f.Types, " ") + " to " + dest
	}
	var counters map[string]map[string]int
	if m := powerGEGate.FindStringSubmatch(levelb.ZoneChangeFilter(t)); m != nil {
		if need, err := strconv.Atoi(m[1]); err == nil && need > f.Power() {
			counters = map[string]map[string]int{"__SOURCE__": {"P1P1": need - f.Power()}}
		}
	}
	if counters == nil && f.IsCreature() && f.Toughness() <= 0 && !strings.Contains(f.PT, "*") {
		// A printed 0/0 (its size comes from a static the board must supply)
		// would die as a state-based action before the probe resolves.
		counters = map[string]map[string]int{"__SOURCE__": {"P1P1": 1 - f.Toughness()}}
	}
	var out []triggerCause
	for _, probe := range probes {
		if c, ok := castCause(reg, name, probe, "p0:"+name); ok {
			c.counters = counters
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "ltb-self probes for " + dest
	}
	return out, ""
}

// zoneProbesReachGraveyard reports whether a Destination$ admits the
// graveyard: absent, Any, or a list naming it.
func zoneProbesReachGraveyard(dest string) bool {
	for _, d := range strings.Split(strings.ToLower(dest), ",") {
		switch strings.TrimSpace(d) {
		case "", "any", "graveyard":
			return true
		}
	}
	return false
}
