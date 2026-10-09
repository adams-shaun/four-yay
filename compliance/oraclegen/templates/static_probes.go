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
// battlefield beyond Grizzly Bears. A filter naming more words than the cap
// (Spider-Ham, Peter Porker, names 18 subtypes) tests only the first cap many,
// so a static whose affected subset is only among the dropped words is never
// observed: a coverage loss, never a false serve, since staticObserved still
// fires only on a real change.
const maxExtraProbes = 3

// staticProbeTable maps one word of an Affected$ filter to the printed card
// placed on p0's battlefield to stand in for it. The first row for a word
// wins; two words may share a probe (Pirate, Goblin and Outlaw share Swab
// Goblin). A probe is chosen for being near-vanilla: no static or replacement
// of its own, and nothing an idle placement fires that moves its own P/T or
// evergreen keywords -- otherwise a probe's own enter, begin-combat or attack
// trigger would satisfy staticObserved with no static at all. Its P/T and
// keywords are read from the registry, not restated here.
// TestStaticProbeTableIsInert holds every row to that contract by casting each
// probe alone and by placing it and attacking with it. Vehicle names an
// uncrewed Vehicle: its P/T is not in the snapshot (creatures only), so only a
// granted keyword shows on it.
var staticProbeTable = []struct{ word, card string }{
	{"Ally", "South Pole Voyager"},
	{"Angel", "Archway Angel"},
	{"Artifact", "Ornithopter"},
	{"Bat", "Lifecreed Duo"},
	{"Bird", "Lifecreed Duo"},
	{"Cat", "Savannah Lions"},
	{"Demon", "Renegade Demon"},
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
	// A lord whose Affected$ names the Legendary supertype (Serah Farron's
	// "Legendary creatures you control get +2/+2") needs a legendary creature
	// probe; Barktooth Warbeard is a vanilla 6/5 so it cannot move itself.
	{"Legendary", "Barktooth Warbeard"},
	{"Merfolk", "Coral Merfolk"},
	{"Mount", "Gila Courser"},
	{"Mouse", "Veteran Guardmouse"},
	{"Ninja", "Foot Elite"},
	{"Noble", "Charming Prince"},
	{"Outlaw", "Swab Goblin"},
	{"Ox", "Pillarfield Ox"},
	{"Pirate", "Swab Goblin"},
	{"Rabbit", "Vizzerdrix"},
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
	probes  []string // table probes added to p0's battlefield
	dropped []string // filter words whose table probes exceeded the cap
	attack  bool     // the filter selects attacking or tapped permanents: attack with p0's creatures
	self    bool     // the card itself must attack: place it, do not cast it
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
	seenProbes := make(map[string]bool)
	droppedProbes := make(map[string]bool)
	seenDroppedWords := make(map[string]bool)
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
			if row.word != w {
				continue
			}
			if !seenProbes[row.card] {
				seenProbes[row.card] = true
				if len(plan.probes) < maxExtraProbes {
					plan.probes = append(plan.probes, row.card)
				} else {
					droppedProbes[row.card] = true
					plan.dropped = append(plan.dropped, w)
					seenDroppedWords[w] = true
				}
			} else if droppedProbes[row.card] && !seenDroppedWords[w] {
				plan.dropped = append(plan.dropped, w)
				seenDroppedWords[w] = true
			}
			break
		}
	}
	// "Self" only matters beside an attack: the card must be on the
	// battlefield unsick to attack, which a cast one is not.
	plan.self = plan.self && plan.attack
	return plan
}

// staticProbeCapGap names filter words whose table probes were omitted by the
// scenario-size cap. It is used only when the retained probes fail to observe
// the static, so served rows remain served.
func staticProbeCapGap(plan staticProbePlan) string {
	if len(plan.dropped) == 0 {
		return ""
	}
	return "filter names more words than the probe cap (" + strconv.Itoa(len(plan.dropped)) + " dropped: " + strings.Join(plan.dropped, ", ") + ")"
}

// staticProbeSpec is a probe's printed observable state, read from the
// registry: the P/T of a creature ("" for a non-creature, whose P/T the
// snapshot omits) and its keywords under both vocabularies.
type staticProbeSpec struct {
	pt            string
	keywords      string // the evergreen fold
	namedKeywords string // the wider named fold
	creature      bool
	chars         staticChars
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
		spec := staticProbeSpec{
			keywords:      oraclediff.ComparedKeywords(f.Keywords, false),
			namedKeywords: oraclediff.ComparedKeywords(f.Keywords, true),
			creature:      f.IsCreature(),
			chars:         printedStaticChars(f),
		}
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
	words := affectedWords(affected)
	// A static on an attachment (an Aura's EnchantedBy, an Equipment's
	// EquippedBy/AttachedBy, a Fortification's FortifiedBy) lands on the
	// permanent it attaches to, which the attachment prelude's scenario
	// serves; its rows keep the generic reason rather than the narrower ones
	// below, which belong to the probe cluster this ticket derives.
	for _, w := range words {
		switch w {
		case "EnchantedBy", "EquippedBy", "AttachedBy", "FortifiedBy":
			return ""
		}
	}
	for _, w := range words {
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
		if !pumped && oraclediff.ComparedKeywords(kws, true) == "" {
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
