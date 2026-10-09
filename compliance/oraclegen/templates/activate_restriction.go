// Activation-restriction preludes for the level-B activate template. An
// activated ability with no targets is still not offered when its activation
// restriction is false on the bare scenario: IsPresent$ ("Activate only if you
// control a Forest or a Plains"), CheckSVar$ (a threshold, a graveyard count),
// or Activation$ (Delirium/Threshold/Metalcraft). This file derives the board,
// graveyard and turn-history setup such a restriction names, reusing the same
// helpers the trigger and static condition preludes use
// (condition_prelude.go, static_condition.go); the activate template tries the
// resulting setup in addition to the bare one and keeps whichever gorge
// actually offers the ability for.
//
// A restriction shape no setup can reach (a solved Case, the city's blessing,
// a self counters/entry state, an opponent's poison counters) is named as a
// gap so the census counts it as a known restriction rather than the generic
// "no fixture" bucket.
package templates

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// maxRestrictCandidates bounds the cross product of restriction preludes the
// activate template tries. A card with two independent gates (an IsPresent
// board and a CheckSVar count) contributes one candidate set each; the product
// is small and the cap keeps a pathological filter from exploding.
const maxRestrictCandidates = 8

// activateRestriction is what an activated ability's offer gates require of
// the setup: candidate preludes to try (empty when the bare scenario is the
// only one) plus, when a named shape could not be built, the reason.
func activateRestriction(reg *cards.Registry, f *cards.Face, name string, sa *cards.SA) ([]conditionPrelude, string) {
	var sources [][]conditionPrelude
	var gaps []string
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKIsPresent)); spec != "" {
		zone := strings.TrimSpace(sa.ParamStr(cards.PKPresentZone))
		compare := strings.TrimSpace(sa.ParamStr(cards.PKPresentCompare))
		// A Class level-up activator's own gate is IsPresent$
		// Card.Self+classLevel_EQ<k> (cards/kw_class.go). At k=1 the Class
		// already qualifies and the bare scenario covers it; at k>=2 the
		// probe raises the Class through its own level-up activators.
		if k := classLevelPresentEQ(spec); k >= 1 {
			if k == 1 {
				// The bare scenario already satisfies the gate.
			} else if pre, ok := classLevelGatePrelude(f, name, k); ok {
				sources = append(sources, []conditionPrelude{pre})
			} else {
				gaps = append(gaps, "activation restriction: class level ("+spec+")")
			}
		} else {
			pres, gap := activationPresentPrelude(reg, f, name, spec, zone, compare)
			if len(pres) > 0 {
				sources = append(sources, pres)
			} else {
				gaps = append(gaps, gap)
			}
		}
	}
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKIsPresent2)); spec != "" {
		pres, gap := activationPresentPrelude(reg, f, name, spec, strings.TrimSpace(sa.ParamStr(cards.PKPresentZone)), "")
		if len(pres) > 0 {
			sources = append(sources, pres)
		} else {
			gaps = append(gaps, gap)
		}
	}
	if check := strings.TrimSpace(sa.ParamStr(cards.PKCheckSVar)); check != "" {
		pres, gap := activationSVarPrelude(reg, f, check, sa.ParamStr(cards.PKSVarCompare))
		if len(pres) > 0 {
			sources = append(sources, pres)
		} else {
			gaps = append(gaps, gap)
		}
	}
	if act := strings.TrimSpace(sa.ParamStr(cards.PKActivation)); act != "" {
		pre, ok, gap := activationKeywordPrelude(act)
		if ok {
			sources = append(sources, []conditionPrelude{pre})
		} else {
			gaps = append(gaps, gap)
		}
	}
	out := restrictCandidates(sources)
	if len(out) == 0 && len(gaps) > 0 {
		return nil, strings.Join(dedupeStrings(gaps), "; ")
	}
	return out, ""
}

// classLevelGatePrelude is the activation prelude for a Class level-up
// activator gated on classLevel_EQ<k>: activate the Class's own level-up
// abilities 2..k, each followed by resolve, so the gate is true when the probe
// activates. ok is false when the face cannot build the prelude.
func classLevelGatePrelude(f *cards.Face, name string, k int) (conditionPrelude, bool) {
	steps, xab, ok := classLevelPrelude(f, name, k)
	if !ok {
		return conditionPrelude{}, false
	}
	return conditionPrelude{steps: steps, xability: xab}, true
}

// restrictCandidates is the cross product of each source's candidates, merged
// and capped. An empty product is one empty prelude (the bare scenario).
func restrictCandidates(sources [][]conditionPrelude) []conditionPrelude {
	out := []conditionPrelude{}
	for _, src := range sources {
		if len(src) == 0 {
			continue
		}
		if len(out) == 0 {
			out = append(out, src...)
			if len(out) > maxRestrictCandidates {
				out = out[:maxRestrictCandidates]
			}
			continue
		}
		var next []conditionPrelude
		for _, base := range out {
			for _, s := range src {
				next = append(next, mergeConditionPreludes([]conditionPrelude{base, s}))
				if len(next) >= maxRestrictCandidates {
					break
				}
			}
			if len(next) >= maxRestrictCandidates {
				break
			}
		}
		out = next
	}
	// A merge is only worth trying when it adds to the bare scenario; an
	// empty candidate duplicates the bare one.
	kept := out[:0]
	for _, pre := range out {
		if !pre.empty() {
			kept = append(kept, pre)
		}
	}
	return kept
}

func (c conditionPrelude) empty() bool {
	return len(c.hand) == 0 && len(c.battlefield) == 0 && len(c.tapped) == 0 &&
		len(c.graveyard) == 0 && len(c.steps) == 0 && len(c.counters) == 0 &&
		len(c.opponentHand) == 0 && len(c.opponentBattlefield) == 0 && c.life == 0
}

// trailingDigits matches a trailing EQ/GE count ("... | SVarCompare$ EQ5").
var trailingDigits = regexp.MustCompile(`(?:EQ|GE|LE|LT|GT)([0-9]+)$`)

// activationPresentPrelude builds candidate setups for an IsPresent$ filter.
// The filter is a comma list of alternatives (Forge reads it as an OR); each
// alternative that names a placeable permanent contributes its stand-in as its
// own candidate, because any one of them satisfies the whole filter. A bare
// Card.Self alternative is satisfied by the source already on the battlefield;
// a self POWER floor the printed card does not meet (Kitsa, Otterball Elite's
// IsPresent$ Card.powerGE3+Self) is raised with +1/+1 counters on the source;
// a self STATE setup cannot give (ThisTurnEntered, counters) is skipped and
// only reported as a gap when no alternative could be built.
func activationPresentPrelude(reg *cards.Registry, f *cards.Face, name, spec, zone, compare string) ([]conditionPrelude, string) {
	if strings.TrimSpace(zone) == "" {
		zone = "Battlefield"
	}
	n := staticCountFrom(compare)
	var out []conditionPrelude
	selfState := false
	selfOnly := false
	for _, group := range strings.Split(spec, ",") {
		words := affectedWords(group)
		if hasWord(words, "Self") {
			// A Self-anchored group is satisfied (or not) by the source's own
			// characteristics: a power/toughness floor the printed card does
			// not meet is raised with +1/+1 counters on the source, and the
			// static-fixture power floor (a Nessian Asp on the board) would
			// satisfy a DIFFERENT card than the gate names.
			if pre, ok := selfPowerFloorPrelude(group, f); ok {
				out = append(out, pre)
				continue
			}
			if n, kind, ok := selfCounterFloor(group); ok {
				out = append(out, conditionPrelude{counters: map[string]map[string]int{"__SOURCE__": {kind: n}}})
				continue
			}
			if staticFixtureFor(group, 0) == "" {
				selfState = true
				continue
			}
			if selfOnlySatisfied(group) {
				selfOnly = true
				continue
			}
		}
		if pres := activationGroupCandidates(reg, group, zone, n); len(pres) > 0 {
			out = append(out, pres...)
			if len(out) >= maxRestrictCandidates {
				out = out[:maxRestrictCandidates]
			}
		}
	}
	if len(out) == 0 {
		if selfOnly {
			// The source itself already satisfies the filter; no setup is
			// needed and an empty candidate is not emitted (the bare
			// scenario covers it).
			return nil, ""
		}
		if selfState {
			return nil, "activation restriction: self state (" + spec + ")"
		}
		return nil, "activation restriction: board presence (" + spec + ")"
	}
	return out, ""
}

// activationGroupCandidates is the setup a single IsPresent alternative asks
// for: a property-aware creature when the filter names a power/toughness floor,
// else staticPresence, else a registry subtype or a generic card type the fixed
// tables omit. A fixture that names an opponent's battlefield or the exile
// zone carries state the conditionPrelude setup cannot express, so it is not a
// candidate (the row then stays a named restriction gap).
func activationGroupCandidates(reg *cards.Registry, group, zone string, n int) []conditionPrelude {
	if n < 1 {
		n = 1
	}
	if name := creatureForPowerFloor(group); name != "" {
		if pre := placeNames(name, zone, group, n); !pre.empty() {
			return []conditionPrelude{pre}
		}
		return nil
	}
	// The repeated-name presence is the established shape (two Forests for a
	// six-land gate); the distinct-catalogue count is the fallback for a gate
	// whose count the repeated name cannot reach. staticPresence has no hand
	// branch — it would place the count on the battlefield — so a hand gate
	// goes straight to the distinct-catalogue count. Both candidates are
	// offered when both exist: the caller tries them in order, and which
	// shape plays through is the settle's call, not this chooser's.
	var out []conditionPrelude
	if !strings.EqualFold(zone, "Hand") {
		if pre, ok := staticPresence(group, zone, n); ok && fitConditionPrelude(pre) {
			out = append(out, pre.conditionPrelude)
		}
	}
	if n > 1 {
		if pre, ok := distinctPresence(reg, group, zone, n); ok {
			out = append(out, pre)
		}
	}
	if len(out) > 0 {
		return out
	}
	if pre, ok := activationPresence(reg, group, zone, n); ok && fitConditionPrelude(pre) {
		return []conditionPrelude{pre.conditionPrelude}
	}
	return nil
}

// presenceCatalogue lists the distinct corpus cards a presence prelude can
// place, by zone. Every name is a plain permanent (no ETB effect a count gate
// would notice); the registry lookup skips any a corpus pin ever drops.
var presenceCatalogue = []string{
	// creatures first, then other permanents, then lands
	"Grizzly Bears", "Serra Angel", "Wall of Air", "Hypnotic Specter", "Hill Giant",
	"Llanowar Elves", "Colossal Dreadmaw", "Craw Wurm", "Siege Wurm", "Nessian Asp",
	"Elvish Mystic", "Terror of the Peaks", "Isamaru, Hound of Konda",
	"Ornithopter", "Sol Ring", "Glorious Anthem", "Honor of the Pure", "Intangible Virtue",
	"Forest", "Island", "Mountain", "Swamp", "Plains", "Crystal Vein",
	"Sunken Citadel", "Captivating Cave",
}

// handCatalogue lists the distinct corpus cards a hand presence prelude can
// place (Resonating Lute's "Activate only if you have seven or more cards in
// your hand").
var handCatalogue = []string{
	"Shock", "Duress", "Dragon Fodder", "Lightning Bolt", "Cancel",
	"Lava Spike", "Divination", "Memnite", "Ornithopter", "Giant Growth",
}

// distinctPresence places n distinct cards matching the group's base word in
// the zone. ok is false when the zone is not placeable, the catalogue cannot
// cover n after registry lookups, or the group names a word no catalogue card
// carries.
func distinctPresence(reg *cards.Registry, group, zone string, n int) (conditionPrelude, bool) {
	if staticOpposing(group) {
		return conditionPrelude{}, false
	}
	catalogue := presenceCatalogue
	switch {
	case strings.EqualFold(zone, "Hand"):
		catalogue = handCatalogue
	case !strings.EqualFold(zone, "Battlefield") && !strings.EqualFold(zone, "Graveyard"):
		return conditionPrelude{}, false
	}
	// A typed group keeps only the cards that carry the type word (a bare
	// "Creature" filter served with an Ornithopter would undercount).
	var typed []string
	base := filterBaseWord(group)
	// Only a group whose every word the catalogue read serves (a type plus a
	// controller word) is safe to serve from the catalogue: any other
	// qualifier (a counter demand, a colour, "withFlying") belongs to the
	// fixture machinery that built the exact cards for it.
	for _, w := range affectedWords(group) {
		lw := strings.ToLower(w)
		if lw == base || lw == "youctrl" || lw == "youown" || lw == "card" || lw == "permanent" {
			continue
		}
		return conditionPrelude{}, false
	}
	// Only the catalogue's own type bases are served here; any other base
	// (a subtype like Gate) needs the registry subtype lookup the static
	// machinery runs, and a catalogue card would satisfy nothing.
	switch base {
	case "creature", "artifact", "enchantment", "land", "permanent", "card":
	default:
		return conditionPrelude{}, false
	}
	wantType := base != "permanent" && base != "card"
	for _, name := range catalogue {
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		if wantType && !oraclegen.HasType(c.Faces[0], base) {
			continue
		}
		typed = append(typed, name)
	}
	if len(typed) < n {
		return conditionPrelude{}, false
	}
	var pre conditionPrelude
	for _, name := range typed[:n] {
		if strings.EqualFold(zone, "Graveyard") {
			pre.graveyard = append(pre.graveyard, name)
		} else if strings.EqualFold(zone, "Hand") {
			pre.hand = append(pre.hand, name)
		} else {
			pre.battlefield = append(pre.battlefield, name)
		}
	}
	return pre, true
}

// fitConditionPrelude reports whether a static fixture uses only the state a
// conditionPrelude carries. Exile, an opponent's battlefield and the
// "place rather than cast" flag have no conditionPrelude field.
func fitConditionPrelude(pre staticFixture) bool {
	return len(pre.exile) == 0 && len(pre.p1Battlefield) == 0 && !pre.place
}

// creatureForPowerFloor names a creature whose printed power, toughness and
// mana value clear the filter's powerGE<n>/toughnessGE<n>/cmcGE<n> floor, or
// "" when the filter names none. The fixture tables' creature stand-ins
// (Llanowar Elves 1/1, Grizzly Bears 2/2) do not.
func creatureForPowerFloor(group string) string {
	needPower, needToughness, needCmc := 0, 0, 0
	for _, w := range affectedWords(group) {
		lower := strings.ToLower(w)
		if rest, ok := strings.CutPrefix(lower, "cmcge"); ok {
			needCmc = max(needCmc, atoiOr(rest, 0))
		}
		if rest, ok := strings.CutPrefix(lower, "powerge"); ok {
			needPower = max(needPower, atoiOr(rest, 0))
		}
		if rest, ok := strings.CutPrefix(lower, "toughnessge"); ok {
			needToughness = max(needToughness, atoiOr(rest, 0))
		}
	}
	if needPower == 0 && needToughness == 0 && needCmc == 0 {
		return ""
	}
	// Nessian Asp is 4/5 with mana value 5 and Gigantosaurus 10/10 with mana
	// value 5; the largest clears any corpus floor this template meets.
	if needCmc > 5 {
		return ""
	}
	if needPower > 4 || needToughness > 5 {
		return "Gigantosaurus"
	}
	return "Nessian Asp"
}

// selfCounterFloor reads a Self counter gate the setup can raise directly
// (Cryptex's IsPresent$ Card.Self+counters_GE5_UNLOCK): a counters_GE<N>_<KIND>
// word is satisfied by N counters of KIND on the source at setup. ok is false
// when the group names no such floor.
func selfCounterFloor(group string) (n int, kind string, ok bool) {
	lower := strings.ToLower(group)
	i := strings.Index(lower, "counters_ge")
	if i < 0 {
		return 0, "", false
	}
	rest := lower[i+len("counters_ge"):]
	j := strings.IndexByte(rest, '_')
	if j <= 0 {
		return 0, "", false
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil || n < 1 {
		return 0, "", false
	}
	k := strings.ToUpper(strings.TrimSpace(rest[j+1:]))
	if k == "" {
		return 0, "", false
	}
	return n, k, true
}

// selfPowerFloorPrelude raises a Self power/toughness floor the printed card
// does not meet (Kitsa, Otterball Elite's IsPresent$ Card.powerGE3+Self on a
// 1/3) with +1/+1 counters on the source, which raise both halves together.
// ok is false when the filter names no such floor or the printed card already
// clears it.
func selfPowerFloorPrelude(group string, f *cards.Face) (conditionPrelude, bool) {
	needPower, needToughness := 0, 0
	for _, w := range affectedWords(group) {
		lower := strings.ToLower(w)
		if rest, ok := strings.CutPrefix(lower, "powerge"); ok {
			needPower = max(needPower, atoiOr(rest, 0))
		}
		if rest, ok := strings.CutPrefix(lower, "toughnessge"); ok {
			needToughness = max(needToughness, atoiOr(rest, 0))
		}
	}
	if needPower == 0 && needToughness == 0 {
		return conditionPrelude{}, false
	}
	curPower, curToughness, ok := strings.Cut(strings.TrimSpace(f.PT), "/")
	if !ok {
		return conditionPrelude{}, false
	}
	power, tough := atoiOr(curPower, 0), atoiOr(curToughness, 0)
	need := max(needPower-power, needToughness-tough)
	if need <= 0 {
		return conditionPrelude{}, false
	}
	return conditionPrelude{counters: map[string]map[string]int{"__SOURCE__": {"P1P1": need}}}, true
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return fallback
}

// placeNames builds the prelude holding n cards of name in the zone. An
// opponent's battlefield is not expressible in a conditionPrelude, so it
// yields the empty prelude.
func placeNames(name, zone, group string, n int) conditionPrelude {
	var out conditionPrelude
	for i := 0; i < n; i++ {
		switch {
		case strings.EqualFold(zone, "Graveyard"):
			out.graveyard = append(out.graveyard, name)
		case strings.EqualFold(zone, "Hand"):
			out.hand = append(out.hand, name)
		case staticOpposing(group):
			return conditionPrelude{}
		default:
			out.battlefield = append(out.battlefield, name)
		}
	}
	return out
}

// selfOnlySatisfied reports whether a Card.Self alternative is one the source
// permanent already satisfies: its words are Self plus card type, supertype or
// colour words, all of which the source's own printed characteristics carry.
// A state word (tapped, attacking, counters, ThisTurnEntered, IsSolved) makes
// it false.
func selfOnlySatisfied(group string) bool {
	for _, w := range affectedWords(group) {
		switch strings.ToLower(w) {
		case "self", "card", "permanent":
			continue
		case "creature", "artifact", "enchantment", "land", "planeswalker", "instant", "sorcery", "battle", "kindred":
			continue
		case "legendary", "basic", "snow", "token":
			continue
		case "white", "blue", "black", "red", "green", "colorless":
			continue
		}
		return false
	}
	return true
}

// activationPresence is staticPresence with the two gaps the activate
// template needs closed: a PresentZone$ Hand/Exile (staticPresence puts every
// non-graveyard zone on the battlefield) and a subtype or basic land name the
// fixture tables omit. The card list is staticPresence's; only the zone it
// lands in is corrected here.
func activationPresence(reg *cards.Registry, group, zone string, n int) (staticFixture, bool) {
	if zone == "Hand" || zone == "Exile" {
		if pre, ok := staticPresence(group, "Graveyard", n); ok {
			if zone == "Hand" {
				pre.hand = append(pre.hand, pre.graveyard...)
				pre.graveyard = nil
			}
			return pre, true
		}
		return activationSubtypePresence(reg, group, zone, n)
	}
	if pre, ok := staticPresence(group, zone, n); ok {
		return pre, true
	}
	return activationSubtypePresence(reg, group, zone, n)
}

// activationSubtypePresence is staticPresence with a registry fallback for a
// subtype the fixed fixture tables do not name (a Sliver, an Ox) and for the
// basic land names the tables omit (Plains, Swamp).
func activationSubtypePresence(reg *cards.Registry, group, zone string, n int) (staticFixture, bool) {
	if n < 1 {
		n = 1
	}
	var out staticFixture
	for i := 0; i < n; i++ {
		card := ""
		for _, w := range affectedWords(group) {
			if name, ok := basicLandCard(w); ok {
				card = name
			}
		}
		if card == "" {
			for _, w := range affectedWords(group) {
				if name, ok := typeFixtureCard(w); ok {
					card = name
				}
			}
		}
		if card == "" {
			for _, w := range affectedWords(group) {
				for _, sub := range outlawBatch(w) {
					if name, ok := oraclegen.SubtypeCard(reg, sub); ok {
						card = name
						break
					}
				}
			}
		}
		if card == "" {
			return staticFixture{}, false
		}
		switch {
		case strings.EqualFold(zone, "Graveyard"):
			out.graveyard = append(out.graveyard, card)
		case strings.EqualFold(zone, "Hand"):
			out.hand = append(out.hand, card)
		case staticOpposing(group):
			out.p1Battlefield = append(out.p1Battlefield, card)
		default:
			out.battlefield = append(out.battlefield, card)
		}
	}
	return out, true
}

// outlawBatch expands Forge's Outlaw batch word into the five creature
// subtypes it names (effects/filter_word.go outlawSubtypes); any other word is
// its own subtype.
func outlawBatch(word string) []string {
	if strings.EqualFold(word, "Outlaw") {
		return []string{"Assassin", "Mercenary", "Pirate", "Rogue", "Warlock"}
	}
	return []string{word}
}

// typeFixtureCard names a stand-in for the card types the fixed tables omit
// (Enchantment, Battle). A type already in staticFixtureTable or
// staticProbeTable never reaches here.
func typeFixtureCard(word string) (string, bool) {
	switch strings.ToLower(word) {
	case "enchantment":
		return "Glorious Anthem", true
	case "battle":
		return "Invasion of Gobakhan", true
	}
	return "", false
}

// basicLandCard names the basic land card for its type word.
func basicLandCard(word string) (string, bool) {
	switch strings.ToLower(word) {
	case "plains":
		return "Plains", true
	case "island":
		return "Island", true
	case "swamp":
		return "Swamp", true
	case "mountain":
		return "Mountain", true
	case "forest":
		return "Forest", true
	}
	return "", false
}

// activationSVarPrelude builds the setup a CheckSVar$/SVarCompare$ gate needs:
// the board, graveyard or turn history the SVar body counts. It reuses the
// static count fixture for a Count$ body and the shared turn-history preludes
// for the rest; when neither covers the shape it names the gap.
func activationSVarPrelude(reg *cards.Registry, f *cards.Face, check, compare string) ([]conditionPrelude, string) {
	body := strings.TrimSpace(f.SVars[check])
	if body == "" {
		// The CheckSVar$ value is the body inline (Omenport Vigilante).
		body = check
	}
	if life, ok := lifeTotalGate(f, body, compare); ok {
		// The gate reads p0's own life total; setup sets it outright, which
		// both engines take as the seat's starting state.
		return []conditionPrelude{{life: life}}, ""
	}
	n := staticCountFrom(compare)
	// A colours-among-permanents count (Puca's Eye's "Activate only if there
	// are five colors among permanents you control",
	// Count$Valid Permanent.YouCtrl$Colors | SVarCompare$ EQ5): place n
	// permanents of n distinct colours.
	if pre, ok := colourCountPrelude(reg, f, body, compare); ok {
		return []conditionPrelude{pre}, ""
	}
	// A this-turn battlefield entry count by type (Lilypad Village's "Activate
	// only if a Bird, Frog, Otter, or Rat entered the battlefield under your
	// control this turn", Count$ThisTurnEntered_Battlefield <types>): move a
	// card of one of the named subtypes from the hand onto the battlefield,
	// which is a real mid-turn entry.
	if pre, ok := thisTurnEnteredPrelude(reg, f, body); ok {
		return []conditionPrelude{pre}, ""
	}
	// A distinct-permanent-card-types count (Matzalantli, the Great Door's
	// delirium gate, Count$ValidGraveyard Card.YouOwn$CardTypesPermanent |
	// SVarCompare$ GE4): place n cards of n distinct permanent types in the
	// named zone.
	if pre, ok := cardTypesPrelude(reg, f, body, n); ok {
		return []conditionPrelude{pre}, ""
	}
	// A type count over two named zones (Cavernous Maw's "Activate only if
	// the number of other Caves you control plus the number of Cave cards in
	// your graveyard is three or greater",
	// Count$ValidGraveyard,Battlefield Cave.YouOwn+Other): place n cards of
	// the type across the two zones, other than the source.
	if pre, ok := twoZoneTypePrelude(reg, f, body, n); ok {
		return []conditionPrelude{pre}, ""
	}
	var out []conditionPrelude
	if pre, ok := staticBodyFixture(reg, body, n); ok {
		out = append(out, pre.conditionPrelude)
	}
	// Turn-history gates the shared prelude classifier already knows: a cast,
	// a lifegain, an attack, a graveyard entry. Narrow the SVar table to the
	// one body so an unrelated SVar cannot steer the classifier.
	for _, c := range conditionPreludes(reg, map[string]string{"CheckSVar": check}, map[string]string{check: body}) {
		if len(c.tapped) > 0 || len(c.counters) > 0 {
			continue
		}
		out = append(out, c)
	}
	out = append(out, historyPreludes(reg, f.Name, body, n)...)
	if lifeLossBody(body) {
		if step, ok := castProbe(reg, "Shock", "p1"); ok {
			out = append(out, conditionPrelude{hand: []string{"Shock"}, steps: []oraclegen.Step{step, {Op: "resolve"}}})
		}
	}
	if len(out) > 0 {
		if len(out) > maxRestrictCandidates {
			out = out[:maxRestrictCandidates]
		}
		return out, ""
	}
	return nil, "activation restriction: SVar (" + svarLabel(check, body) + ")"
}

// filterBaseWord is the first comma-separated alternative's head word,
// lowercased (the oraclegen-side helper is unexported there).
func filterBaseWord(group string) string {
	first := strings.SplitN(group, ",", 2)[0]
	return strings.ToLower(strings.SplitN(strings.TrimSpace(first), ".", 2)[0])
}

// colourCountPrelude serves a Count$Valid <filter>$Colors gate at EQ<n>
// (Puca's Eye's five colours among permanents you control): n permanents of n
// distinct colours, in fixed colour order. The source itself counts only if
// it is coloured, which these probes' sources are not.
func colourCountPrelude(reg *cards.Registry, f *cards.Face, body, compare string) (conditionPrelude, bool) {
	// staticCountFrom reads only GE forms; the gate here is an EQ (Puca's
	// Eye's SVarCompare$ EQ5), so take the count from the compare's digits.
	n := 0
	if m := trailingDigits.FindStringSubmatch(strings.TrimSpace(compare)); m != nil {
		if v, err := strconv.Atoi(m[1]); err == nil {
			n = v
		}
	}
	if n < 1 || !strings.HasSuffix(strings.TrimSpace(body), "$Colors") || !strings.Contains(body, "Count$Valid") {
		return conditionPrelude{}, false
	}
	colours := []string{"Serra Angel", "Wall of Air", "Hypnotic Specter", "Hill Giant", "Grizzly Bears"}
	var pre conditionPrelude
	placed := 0
	for _, name := range colours {
		if placed >= n {
			break
		}
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		placed++
		pre.battlefield = append(pre.battlefield, name)
	}
	if placed < n {
		return conditionPrelude{}, false
	}
	return pre, true
}

// thisTurnEnteredPrelude serves a Count$ThisTurnEntered_Battlefield <types>
// gate (Lilypad Village's Bird, Frog, Otter or Rat): move a card of one of
// the named subtypes from the hand onto the battlefield, a real mid-turn
// entry both engines count. A gate over another destination (Macabre
// Reconstruction's "a creature card was put into your graveyard from anywhere
// this turn", Count$ThisTurnEntered_Graveyard) is NOT served: a battlefield
// entry satisfies nothing there.
func thisTurnEnteredPrelude(reg *cards.Registry, f *cards.Face, body string) (conditionPrelude, bool) {
	i := strings.Index(body, "ThisTurnEntered_")
	if i < 0 {
		return conditionPrelude{}, false
	}
	rest := body[i+len("ThisTurnEntered_"):]
	// The head is Count$ThisTurnEntered_<Dest>[_from_<Origin>]_<Valid>: only
	// a battlefield destination is a move-onto-the-battlefield entry.
	dest, filter, ok := strings.Cut(rest, "_")
	if !ok || !strings.EqualFold(dest, "Battlefield") {
		return conditionPrelude{}, false
	}
	// An origin qualifier (ThisTurnEntered_Battlefield_from_Hand_Card...) is
	// satisfied by the same hand-to-battlefield move; drop it.
	if origin, after, ok := strings.Cut(filter, "_"); ok && strings.EqualFold(origin, "from") {
		if _, after, ok := strings.Cut(after, "_"); ok {
			filter = after
		}
	}
	for _, alt := range strings.Split(filter, ",") {
		sub := filterBaseWord(alt)
		if sub == "" {
			continue
		}
		name, ok := oraclegen.SubtypeCard(reg, sub)
		if !ok || strings.EqualFold(name, f.Name) {
			continue
		}
		return conditionPrelude{hand: []string{name},
			steps: []oraclegen.Step{{Op: "move", Seat: 0, Card: "p0:" + name, To: "battlefield"}}}, true
	}
	return conditionPrelude{}, false
}

// cardTypesPrelude serves a Count$Valid<Zone> <filter>$CardTypesPermanent
// gate at GE<n> (Matzalantli, the Great Door's delirium): n cards of n
// distinct permanent card types in the named zone.
func cardTypesPrelude(reg *cards.Registry, f *cards.Face, body string, n int) (conditionPrelude, bool) {
	if n < 1 || !strings.Contains(body, "$CardTypesPermanent") {
		return conditionPrelude{}, false
	}
	zone := ""
	if strings.Contains(body, "ValidGraveyard") {
		zone = "Graveyard"
	} else if strings.Contains(body, "ValidBattlefield") {
		zone = "Battlefield"
	} else {
		return conditionPrelude{}, false
	}
	types := []string{"Grizzly Bears", "Sol Ring", "Forest", "Glorious Anthem", "Jace Beleren", "Plains", "Island", "Swamp", "Mountain"}
	seen := map[string]bool{}
	var pre conditionPrelude
	for _, name := range types {
		if len(seen) >= n {
			break
		}
		c, ok := reg.Lookup(name)
		if !ok || len(c.Faces) == 0 {
			continue
		}
		kind := ""
		for _, ty := range c.Faces[0].Types {
			kind = ty
			break
		}
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		if zone == "Graveyard" {
			pre.graveyard = append(pre.graveyard, name)
		} else {
			pre.battlefield = append(pre.battlefield, name)
		}
	}
	if len(seen) < n {
		return conditionPrelude{}, false
	}
	return pre, true
}

// twoZoneTypePrelude serves a Count$Valid<Zone1>,<Zone2> <Type>.<quals> gate
// at GE<n> (Cavernous Maw's other-Caves-you-control-plus-Caves-in-graveyard):
// n distinct cards of the type other than the source, split one-into-the-
// graveyard-first when the zones name it.
func twoZoneTypePrelude(reg *cards.Registry, f *cards.Face, body string, n int) (conditionPrelude, bool) {
	if n < 1 || !strings.Contains(body, "Count$Valid") {
		return conditionPrelude{}, false
	}
	rest := body[strings.Index(body, "Count$Valid")+len("Count$Valid"):]
	zoneStr, filter, ok := strings.Cut(rest, " ")
	if !ok || !strings.Contains(zoneStr, "Graveyard") || !strings.Contains(zoneStr, "Battlefield") {
		return conditionPrelude{}, false
	}
	sub := filterBaseWord(filter)
	if sub == "" {
		return conditionPrelude{}, false
	}
	var pre conditionPrelude
	placed := 0
	for _, name := range subtypeCards(reg, sub, f.Name, n) {
		placed++
		if placed == 1 {
			pre.graveyard = append(pre.graveyard, name)
			continue
		}
		pre.battlefield = append(pre.battlefield, name)
	}
	if placed < n {
		return conditionPrelude{}, false
	}
	return pre, true
}

// subtypeCards lists up to n distinct corpus cards whose front face carries
// the subtype, excluding avoid. Mirrors registrySubtype's walk (front face
// only, XMage-known names only) but collects several.
func subtypeCards(reg *cards.Registry, subtype, avoid string, n int) []string {
	if reg == nil || n < 1 {
		return nil
	}
	var out []string
	for i := 0; i < reg.Len() && len(out) < n; i++ {
		c := reg.Card(i)
		if len(c.Faces) == 0 {
			continue
		}
		f := c.Faces[0]
		if strings.EqualFold(f.Name, avoid) || !oraclegen.XMageKnown(f.Name) {
			continue
		}
		hit := false
		for _, ty := range f.Types {
			hit = hit || strings.EqualFold(strings.TrimSpace(ty), subtype)
		}
		if hit {
			out = append(out, f.Name)
		}
	}
	return out
}

// startingLife is the two-player starting life total both engines deal
// (CR 103.4); Count$YourStartingLife reads it.
const startingLife = 20

// lifeTotalGate reads an activation restriction on p0's own life total
// (CheckSVar$ <V> with V = Count$YourLifeTotal) and returns the life total
// that satisfies it: SVarCompare$ GE<n> / GT<n> with n a literal or an SVar
// counting Count$YourStartingLife[/Plus.k] (Ayli, Eternal Pilgrim and
// Speaker of the Heavens: "at least k life more than your starting life
// total"). ok is false for any other body or comparison.
func lifeTotalGate(f *cards.Face, body, compare string) (int32, bool) {
	if !strings.EqualFold(strings.TrimSpace(body), "Count$YourLifeTotal") {
		return 0, false
	}
	compare = strings.TrimSpace(compare)
	if len(compare) < 3 {
		return 0, false
	}
	op, rhs := strings.ToUpper(compare[:2]), compare[2:]
	n, ok := lifeGateValue(f, rhs)
	if !ok {
		return 0, false
	}
	switch op {
	case "GE":
	case "GT":
		n++
	default:
		return 0, false
	}
	if n < 1 {
		return 0, false
	}
	return int32(n), true
}

// lifeGateValue evaluates a life-gate comparand: a literal, or an SVar whose
// body is Count$YourStartingLife with an optional /Plus.k offset.
func lifeGateValue(f *cards.Face, rhs string) (int, bool) {
	if n, err := strconv.Atoi(strings.TrimSpace(rhs)); err == nil {
		return n, true
	}
	body := strings.TrimSpace(f.SVars[strings.TrimSpace(rhs)])
	head, offset, hasOffset := strings.Cut(body, "/")
	if !strings.EqualFold(head, "Count$YourStartingLife") {
		return 0, false
	}
	if !hasOffset {
		return startingLife, true
	}
	k, ok := strings.CutPrefix(offset, "Plus.")
	if !ok {
		return 0, false
	}
	v, err := strconv.Atoi(k)
	if err != nil {
		return 0, false
	}
	return startingLife + v, true
}

// lifeLossBody reports whether an SVar body counts life an opponent lost or
// damage dealt to an opponent this turn: the shared turn-history preludes do
// not cover these, so a Shock at the opponent supplies the event. The needles
// are separate substrings because "wasDealtNonCombatDamageThisTurn" does not
// contain "wasDealtDamageThisTurn" contiguously.
func lifeLossBody(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "lifeoppslostthisturn") ||
		(strings.Contains(lower, "wasdealt") && strings.Contains(lower, "damagethisturn"))
}

// svarLabel names an unbuilt CheckSVar shape for the census: the body's head
// when it is an inline expression, else the SVar name.
func svarLabel(check, body string) string {
	if body == "" {
		return check
	}
	if strings.Contains(body, "$") || strings.HasPrefix(strings.ToLower(body), "count") ||
		strings.HasPrefix(strings.ToLower(body), "player") {
		return strings.Fields(body)[0]
	}
	return check
}

// activationKeywordPrelude builds the setup an Activation$ keyword gate needs.
func activationKeywordPrelude(act string) (conditionPrelude, bool, string) {
	switch strings.ToLower(strings.TrimSpace(act)) {
	case "threshold":
		// CR 702.16: seven or more cards in your graveyard.
		return conditionPrelude{graveyard: oraclegen.Repeat("Wastes", 7)}, true, ""
	case "delirium":
		// CR 702.174: four or more card types among cards in your graveyard.
		return conditionPrelude{graveyard: bigGraveyard}, true, ""
	case "metalcraft":
		// CR 702.102: three or more artifacts.
		return conditionPrelude{battlefield: []string{"Sol Ring", "Arcane Signet", "Ornithopter"}}, true, ""
	case "hellbent":
		// CR 702.90: no cards in hand. The fixture asks for the ability to be
		// offered, and an ability with no hand cost adds no cards, so the
		// empty candidate leaves the bare hand.
		return conditionPrelude{}, true, ""
	}
	return conditionPrelude{}, false, "activation restriction: activation " + strings.TrimSpace(act)
}

// mergeConditionPreludes folds several candidates into the one setup holding
// everything they hold, with the counters map merged deterministically.
func mergeConditionPreludes(parts []conditionPrelude) conditionPrelude {
	var out conditionPrelude
	for _, p := range parts {
		out.hand = append(out.hand, p.hand...)
		out.battlefield = append(out.battlefield, p.battlefield...)
		out.tapped = append(out.tapped, p.tapped...)
		out.graveyard = append(out.graveyard, p.graveyard...)
		out.steps = append(out.steps, p.steps...)
		out.xability = append(out.xability, p.xability...)
		out.life = max(out.life, p.life)
		for card, kinds := range p.counters {
			if out.counters == nil {
				out.counters = map[string]map[string]int{}
			}
			if out.counters[card] == nil {
				out.counters[card] = map[string]int{}
			}
			for kind, n := range kinds {
				out.counters[card][kind] += n
			}
		}
	}
	return out
}

// applyActivationPrelude adds the prelude's cards to p0 and returns the steps
// that must run before the activate step. Self counters are placed on the
// source card.
func applyActivationPrelude(p0 oraclegen.Seat, name string, pre conditionPrelude) (oraclegen.Seat, []oraclegen.Step) {
	p0.Hand = append(p0.Hand, pre.hand...)
	p0.Graveyard = append(p0.Graveyard, pre.graveyard...)
	if pre.life > 0 {
		life := pre.life
		p0.Life = &life
	}
	for _, card := range pre.battlefield {
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
	}
	for _, card := range pre.tapped {
		if card == "__SOURCE__" {
			card = name
		}
		p0.Battlefield = appendFixtureUnique(p0.Battlefield, card)
		p0.Tapped = appendFixtureUnique(p0.Tapped, card)
	}
	for _, card := range sortedCounterCards(pre.counters) {
		target := card
		if target == "__SOURCE__" {
			target = name
		}
		for _, kind := range sortedCounterKinds(pre.counters[card]) {
			p0 = oraclegen.WithCounters(p0, target, kind, int32(pre.counters[card][kind]))
		}
	}
	return p0, append([]oraclegen.Step(nil), pre.steps...)
}

func sortedCounterCards(m map[string]map[string]int) []string {
	out := make([]string, 0, len(m))
	for card := range m {
		out = append(out, card)
	}
	sort.Strings(out)
	return out
}

func sortedCounterKinds(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for kind := range m {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

// dedupeStrings keeps the first occurrence of each string in order.
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
