// Probe selection for the level-B continuous-static template (ticket
// levelb-static-probe-from-filter). A lord whose Affected$ names a subtype, a
// non-creature type or a state Grizzly Bears lacks pumps nothing observable,
// so the template retries with the probes this file derives from the filter's
// words. There is no second filter evaluator: a probe the filter does not
// select simply shows no change, and staticObserved decides.
package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclediff"
)

// maxExtraProbes bounds how many table probes one scenario adds to p0's
// battlefield beyond Grizzly Bears.
const maxExtraProbes = 3

// staticProbeTable maps one word of an Affected$ filter to the printed card
// placed on p0's battlefield to stand in for it. The first row for a word
// wins; two words may share a probe (Pirate, Goblin and Outlaw share Swab
// Goblin). A probe is chosen for being near-vanilla (nothing that fires while
// idle, no static of its own), present in the Forge corpus and printed in a
// set both engines know; its P/T and keywords are read from the registry, not
// restated here. Vehicle names an uncrewed Vehicle: its P/T is not in the
// snapshot (creatures only), so only a granted keyword shows on it.
var staticProbeTable = []struct{ word, card string }{
	{"Ally", "South Pole Voyager"},
	{"Angel", "Archway Angel"},
	{"Artifact", "Ornithopter"},
	{"Bat", "Lifecreed Duo"},
	{"Bird", "Lifecreed Duo"},
	{"Cat", "Savannah Lions"},
	{"Demon", "Gurmag Rakshasa"},
	{"Detective", "Tunnel Surveyor"},
	{"Dinosaur", "Gigantosaurus"},
	{"Dragon", "Shivan Dragon"},
	{"Elf", "Fierce Empath"},
	{"Frog", "Pond Prophet"},
	{"Giant", "Skyraker Giant"},
	{"Goblin", "Swab Goblin"},
	{"Hero", "Hero in Training"},
	{"Kithkin", "Eclipsed Kithkin"},
	{"Land", "Dryad Arbor"},
	{"Merfolk", "Cenote Scout"},
	{"Mount", "Gila Courser"},
	{"Mouse", "Pests of Honor"},
	{"Ninja", "Foot Elite"},
	{"Noble", "Charming Prince"},
	{"Outlaw", "Swab Goblin"},
	{"Ox", "Spirit Mascot"},
	{"Pirate", "Swab Goblin"},
	{"Rabbit", "Nasty Little Rabbit"},
	{"Rhino", "Zoo Escapees"},
	{"Skeleton", "Skeleton Archer"},
	{"Sliver", "Metallic Sliver"},
	{"Spider", "Mineshaft Spider"},
	{"Squirrel", "Curious Forager"},
	{"Thopter", "Ornithopter"},
	{"Turtle", "Aegis Turtle"},
	{"Vampire", "Vampire Spawn"},
	{"Vehicle", "Dune Drifter"},
	{"Villain", "Common Crook"},
	{"Zombie", "Maalfeld Twins"},
	// A filter on a granted-or-printed evergreen property selects a creature
	// that has it.
	{"withFlying", "Ornithopter"},
}

// staticProbePlan is what a filter asks of the scenario beyond the Bear.
type staticProbePlan struct {
	probes []string // table probes added to p0's battlefield
	attack bool     // the filter selects attacking or tapped permanents: attack with p0's creatures
	self   bool     // the card itself must attack: place it, do not cast it
}

func (p staticProbePlan) empty() bool {
	return len(p.probes) == 0 && !p.attack && !p.self
}

// affectedWords splits an Affected$ filter into its words: the type, subtype,
// state and controller tokens, with ",", "+", "." and spaces as separators.
func affectedWords(affected string) []string {
	return strings.FieldsFunc(affected, func(r rune) bool {
		return r == '.' || r == '+' || r == ',' || r == ' '
	})
}

// staticPlanFor derives the probe plan from a static's Affected$ filter.
func staticPlanFor(affected string) staticProbePlan {
	var plan staticProbePlan
	words := affectedWords(affected)
	for _, w := range words {
		switch {
		// A setup-tapped p0 permanent is untapped by p0's first untap step,
		// so "tapped" is reached the way a creature taps in play: it attacks.
		case w == "tapped" || w == "attacking":
			plan.attack = true
		case w == "Self":
			plan.self = true
		}
		for _, row := range staticProbeTable {
			if row.word == w {
				plan.probes = appendFixtureUnique(plan.probes, row.card)
				break
			}
		}
	}
	// "Self" only matters beside an attack: the card must be on the
	// battlefield unsick to attack, which a cast one is not.
	plan.self = plan.self && plan.attack
	if len(plan.probes) > maxExtraProbes {
		plan.probes = plan.probes[:maxExtraProbes]
	}
	return plan
}

// staticProbeSpec is a probe's printed observable state, read from the
// registry: the P/T of a creature ("" for a non-creature, whose P/T the
// snapshot omits) and its evergreen keywords.
type staticProbeSpec struct {
	pt       string
	keywords string
	creature bool
}

// staticProbeSpecs reads each probe's printed spec. A probe missing from the
// corpus is absent from the result, so its permanent is never mistaken for a
// changed one.
func staticProbeSpecs(reg *cards.Registry, names []string) map[string]staticProbeSpec {
	specs := make(map[string]staticProbeSpec, len(names))
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		f := c.Faces[0]
		spec := staticProbeSpec{keywords: oraclediff.EvergreenKeywords(f.Keywords), creature: f.IsCreature()}
		if spec.creature {
			spec.pt = f.PT
		}
		specs[n] = spec
	}
	return specs
}

// staticGap names why a static whose probes showed no change is unobservable,
// when the cause is a known shape rather than the generic "nothing changed":
// a qualifier the setup cannot give a probe, a grant outside the compared
// evergreen keyword set, or an amount counted from something the fixture
// leaves at zero. "" is the generic reason.
func staticGap(st cards.Static, affected string) string {
	for _, w := range affectedWords(affected) {
		switch {
		case strings.HasPrefix(w, "counters_") || w == "HasCounters":
			return "needs counters on the affected permanent"
		case w == "token":
			return "needs a token (setup places none)"
		case w == "equipped":
			return "needs an equipped permanent"
		case w == "modified":
			return "needs a modified permanent"
		}
	}
	pumps := []cards.ParamKey{cards.PKAddPower, cards.PKAddToughness, cards.PKSetPower, cards.PKSetToughness}
	if st.HasParam(cards.PKAddKeyword) {
		kws := strings.Split(st.ParamStr(cards.PKAddKeyword), " & ")
		pumped := false
		for _, k := range pumps {
			pumped = pumped || st.HasParam(k)
		}
		if !pumped && oraclediff.EvergreenKeywords(kws) == "" {
			return "grants only keywords outside the compared evergreen set"
		}
	}
	for _, k := range pumps {
		if !st.HasParam(k) {
			continue
		}
		if _, err := strconv.Atoi(st.ParamStr(k)); err != nil {
			return "amount is a computed count the fixture does not make observable"
		}
	}
	return ""
}
