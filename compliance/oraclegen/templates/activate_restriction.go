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
		} else if solvedSelfSpec(spec) {
			// IsPresent$ Card.Self+IsSolved: the ability is offered only once
			// the source Case has solved itself at its end step.
			if pres, ok := solvedCasePreludes(reg, f); ok {
				sources = append(sources, pres)
			} else {
				gaps = append(gaps, "activation restriction: self state ("+spec+")")
			}
		} else {
			pres, gap := activationPresentPrelude(reg, spec, zone, compare)
			if len(pres) > 0 {
				sources = append(sources, pres)
			} else {
				gaps = append(gaps, gap)
			}
		}
	}
	if spec := strings.TrimSpace(sa.ParamStr(cards.PKIsPresent2)); spec != "" {
		pres, gap := activationPresentPrelude(reg, spec, strings.TrimSpace(sa.ParamStr(cards.PKPresentZone)), "")
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
		if strings.EqualFold(act, "Solved") {
			// Activation$ Solved: the ability is offered only once the source
			// Case has solved itself at its end step.
			if pres, ok := solvedCasePreludes(reg, f); ok {
				sources = append(sources, pres)
			} else {
				gaps = append(gaps, "activation restriction: activation Solved")
			}
		} else {
			pre, ok, gap := activationKeywordPrelude(act)
			if ok {
				sources = append(sources, []conditionPrelude{pre})
			} else {
				gaps = append(gaps, gap)
			}
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

// activationPresentPrelude builds candidate setups for an IsPresent$ filter.
// The filter is a comma list of alternatives (Forge reads it as an OR); each
// alternative that names a placeable permanent contributes its stand-in as its
// own candidate, because any one of them satisfies the whole filter. A bare
// Card.Self alternative is satisfied by the source already on the battlefield;
// a self STATE setup cannot give (ThisTurnEntered, counters) is skipped and
// only reported as a gap when no alternative could be built.
func activationPresentPrelude(reg *cards.Registry, spec, zone, compare string) ([]conditionPrelude, string) {
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
			if m := counterFilterRE.FindStringSubmatch(strings.ToLower(group)); m != nil {
				// Counters on the source itself (Cryptex's "five or more
				// unlock counters"): the setup carries them.
				out = append(out, conditionPrelude{counters: map[string]map[string]int{"__SOURCE__": {strings.ToUpper(m[2]): counterAmount(strings.ToLower(group))}}})
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
	if pre, ok := staticPresence(group, zone, n); ok && fitConditionPrelude(pre) {
		return []conditionPrelude{pre.conditionPrelude}
	}
	if pre, ok := activationPresence(reg, group, zone, n); ok && fitConditionPrelude(pre) {
		return []conditionPrelude{pre.conditionPrelude}
	}
	return nil
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
		out.opponentHand = append(out.opponentHand, p.opponentHand...)
		out.opponentBattlefield = append(out.opponentBattlefield, p.opponentBattlefield...)
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
	p0.Battlefield = appendFixtureCounts(p0.Battlefield, pre.battlefield)
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
