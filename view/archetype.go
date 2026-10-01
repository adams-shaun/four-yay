package view

import (
	"strings"
	"unicode/utf8"

	"github.com/adams-shaun/gorge/state"
)

// This file is the CR-visible half of the observability checklist's
// opp_archetype_posterior fact (internal/botobs/checklist.json): a per-seat
// posterior over what an opponent's deck is trying to do, inferred purely
// from the cards REVEALED about that seat.
//
// It is a projection, not a tracker. Nothing here persists across decisions;
// the posterior is a pure function of the PlayerView's own public card lists
// (battlefield, graveyard, exile, command zone), which the projection has
// already decided that viewer may see under CR 400.2. That keeps the view
// tier free of hidden information by construction: a hidden hand is never a
// CardView at all, so it can never be classified, and the classifier reads
// no card NAME -- only the printed, public shape (mana symbols, card types,
// the spell ability's API, power and mana production).
//
// The heuristic is deliberately small and honest: colour priors plus curve,
// speed and spell-class signals, normalised to a probability vector. It is
// the same idea internal/spellbench/builtins/tactical_archetype.go applies
// inside the SpellBench type-gauntlet, reimplemented here because the view
// tier is BELOW that package in the dependency order (view may not import
// internal/spellbench) and because a view-shaped posterior must not depend
// on a policy tier that can be switched off.

// Archetype names, dense and stable. The wire string is the name itself; no
// map iteration ever reaches a caller, so the ordering cannot leak.
const (
	ArchAggro = iota
	ArchBurn
	ArchTempo
	ArchControl
	ArchRamp
	ArchGoWide
	ArchMidrange
	archKindCount
)

// ArchetypeName returns the wire name for a dense archetype index, or "".
func ArchetypeName(i int) string {
	if i < 0 || i >= archKindCount {
		return ""
	}
	return archetypeNames[i]
}

var archetypeNames = [archKindCount]string{
	"aggro", "burn", "tempo", "control", "ramp", "go-wide", "midrange",
}

// ArchetypePosterior is a posterior over an opponent's deck archetype,
// inferred from the cards revealed about that seat. It is nil for the
// viewer's own seat and for a spectator: the fact is an OPPONENT archetype
// posterior, and a seat's own deck identity is already the manifest's
// job (deck.File.Archetype), not something to re-infer from its graveyard.
//
// Known is false when nothing classifiable has been revealed yet; Scores is
// then empty and Top is "". A Known posterior's Scores entries sum to 1 and
// carry only positive values, so an absent entry means a zero posterior.
type ArchetypePosterior struct {
	// Known reports whether any revealed card carried a classifiable signal.
	Known bool `json:"known"`
	// Seen is how many distinct revealed cards were classified.
	Seen int `json:"seen"`
	// Top is the highest-probability archetype's name ("" when !Known).
	Top string `json:"top,omitempty"`
	// TopScore is that archetype's posterior probability.
	TopScore float64 `json:"top_score,omitempty"`
	// Scores is the full normalised posterior, keyed by archetype name. It is
	// built with a fixed key order (dense index order) and only ever read by
	// key, so no map-range order can reach the wire.
	Scores map[string]float64 `json:"scores,omitempty"`
}

// inferArchetypePosterior classifies the cards revealed about one seat. It
// returns nil when there is nothing to classify, which is the honest answer
// for a seat whose public zones are still empty.
func inferArchetypePosterior(zones ...[]CardView) *ArchetypePosterior {
	return inferArchetypePosteriorZones(nil, zones)
}

// inferArchetypePosteriorInto is inferArchetypePosterior over a seat's four
// revealed public zones, written into dst's storage (its struct and its
// Scores map) when dst is non-nil -- ProjectInto's reuse.
func inferArchetypePosteriorInto(dst *ArchetypePosterior, battlefield, graveyard, exile, command []CardView) *ArchetypePosterior {
	zones := [4][]CardView{battlefield, graveyard, exile, command}
	return inferArchetypePosteriorZones(dst, zones[:])
}

// archSeenBits bounds the object ids inferArchetypePosteriorZones dedupes
// with its stack bitset; a larger id falls back to scanning the cards
// already classified, which answers the same question.
const archSeenBits = 1 << 13

func inferArchetypePosteriorZones(dst *ArchetypePosterior, zones [][]CardView) *ArchetypePosterior {
	var acc archetypeAcc
	var seen [archSeenBits / 64]uint64
	count := 0
	for zi, zone := range zones {
		for i := range zone {
			cv := &zone[i]
			if cv.FaceDown {
				// A face-down card reveals no identity: it is a 2/2 with no
				// name, so classifying it would read the projection's own
				// blanking as a signal. Skip it.
				continue
			}
			if cv.ID != 0 && archSeen(&seen, zones, zi, i, cv.ID) {
				// The same object projected twice (a commander listed in both
				// Commanders and the command zone) is one revealed card.
				continue
			}
			count++
			acc.accumulate(cv)
		}
	}
	if acc.empty() {
		return nil
	}
	return acc.posterior(dst, count)
}

// archSeen reports whether id was already classified -- an earlier face-up
// card in zones (before zones[zi][i]) carried it -- and marks it seen.
func archSeen(seen *[archSeenBits / 64]uint64, zones [][]CardView, zi, i int, id state.ObjID) bool {
	if id < archSeenBits {
		w, bit := id>>6, uint64(1)<<(id&63)
		if seen[w]&bit != 0 {
			return true
		}
		seen[w] |= bit
		return false
	}
	for z := 0; z <= zi; z++ {
		end := len(zones[z])
		if z == zi {
			end = i
		}
		for j := 0; j < end; j++ {
			if c := &zones[z][j]; !c.FaceDown && c.ID == id {
				return true
			}
		}
	}
	return false
}

// archetypeAcc is the accumulated public feature set across a seat's
// revealed cards. Dense counters only.
type archetypeAcc struct {
	colors [5]bool // W U B R G
	anyCol bool

	creatures   int
	mvSum       int
	fastBodies  int
	smallBodies int
	evasion     int
	artifacts   int
	burnFace    int
	counters    int
	removal     int
	flow        int
	ramp        int
	tokens      int
	manaSources int
}

func (a *archetypeAcc) anyColor() bool {
	for i := range a.colors {
		if a.colors[i] {
			return true
		}
	}
	return false
}

func (a *archetypeAcc) empty() bool {
	return !a.anyColor() && a.creatures == 0 && a.artifacts == 0 &&
		a.burnFace == 0 && a.counters == 0 && a.removal == 0 &&
		a.flow == 0 && a.ramp == 0 && a.tokens == 0
}

// accumulate folds one revealed card's public shape into the feature set.
func (a *archetypeAcc) accumulate(cv *CardView) {
	a.colorsFrom(cv)
	if hasTypeWord(cv.Types, "creature") {
		a.creatures++
		mv := cmcFromManaCost(cv.ManaCost)
		a.mvSum += mv
		if mv <= 2 {
			a.fastBodies++
		}
		if cv.Power >= 0 && cv.Power <= 2 {
			a.smallBodies++
		}
	}
	if hasTypeWord(cv.Types, "artifact") {
		a.artifacts++
	}
	if hasTypeWord(cv.Types, "land") {
		// A land contributes colour (its production) but never a spell
		// class; lands are already covered by colorsFrom.
	}
	switch cv.SpellAPI {
	case "DealDamage", "DamageAll", "DamageEachOther", "DamageMulti":
		a.burnFace++
	case "Counter":
		a.counters++
	case "Destroy", "DestroyAll", "Exile", "ExileAll", "ChangeZone", "Sacrifice":
		a.removal++
	case "Draw", "Dig", "Mill", "Tutor", "DigUntil", "RearrangeTopOfLibrary":
		a.flow++
	case "Token":
		a.tokens++
	}
	for _, k := range cv.Keywords {
		if isEvasionKeyword(keywordHead(k)) {
			a.evasion++
		}
	}
	if cv.Produces != nil {
		a.manaSources++
		if cv.Produces.Any {
			for i := range a.colors {
				a.colors[i] = true
			}
			a.anyCol = true
		}
		for i := 0; i < len(a.colors) && i < len(cv.Produces.Colour); i++ {
			if cv.Produces.Colour[i] > 0 {
				a.colors[i] = true
				a.anyCol = true
			}
		}
	}
}

// colorsFrom reads the colour identity from the printed mana cost and the
// projected production. A source making any colour contributes all five, a
// soft signal rather than a rule.
func (a *archetypeAcc) colorsFrom(cv *CardView) {
	for i := 0; i < len(cv.ManaCost); i++ {
		switch cv.ManaCost[i] {
		case 'W':
			a.colors[0], a.anyCol = true, true
		case 'U':
			a.colors[1], a.anyCol = true, true
		case 'B':
			a.colors[2], a.anyCol = true, true
		case 'R':
			a.colors[3], a.anyCol = true, true
		case 'G':
			a.colors[4], a.anyCol = true, true
		}
	}
}

// keywordHead strips a trailing parameter from a keyword token, mirroring
// cards.KeywordHead without importing it: the view tier already treats
// keywords as opaque strings (CardView.Keywords), so a local head split
// keeps that boundary.
// evasionKeywords are the keyword heads accumulate counts as evasion,
// compared case-insensitively (lowerIs).
var evasionKeywords = [...]string{"flying", "menace", "unblockable", "fear", "intimidate", "deathtouch"}

func isEvasionKeyword(head string) bool {
	for _, w := range evasionKeywords {
		if lowerIs(head, w) {
			return true
		}
	}
	return false
}

// hasTypeWord reports whether word (lower-case ASCII) is one of the
// space-separated words of the type line types, compared after
// strings.ToLower -- exactly strings.Contains(" "+strings.ToLower(types)+" ",
// " "+word+" "), without building either string.
func hasTypeWord(types, word string) bool {
	for {
		tok, rest, more := strings.Cut(types, " ")
		if lowerIs(tok, word) {
			return true
		}
		if !more {
			return false
		}
		types = rest
	}
}

// lowerIs reports strings.ToLower(s) == want for a lower-case ASCII want,
// allocation-free for an ASCII s (a non-ASCII s takes ToLower itself, since
// a few non-ASCII runes lower-case into ASCII).
func lowerIs(s, want string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return strings.ToLower(s) == want
		}
	}
	if len(s) != len(want) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != want[i] {
			return false
		}
	}
	return true
}

func keywordHead(k string) string {
	if i := strings.IndexByte(k, ':'); i >= 0 {
		return k[:i]
	}
	return k
}

// cmcFromManaCost converts Forge's "1 W", "R", "2 G G" notation to a mana
// value. Symbol-free generic runs count as their integer; every colour or
// hybrid symbol counts as one. It never panics on a malformed string.
func cmcFromManaCost(cost string) int {
	total, digits := 0, -1
	for i := 0; i < len(cost); i++ {
		c := cost[i]
		if c == ' ' {
			continue
		}
		if c >= '0' && c <= '9' {
			if digits < 0 {
				digits = 0
			}
			digits = digits*10 + int(c-'0')
			continue
		}
		if digits >= 0 {
			total += digits
			digits = -1
		}
		total++
	}
	if digits >= 0 {
		total += digits
	}
	return total
}

// posterior normalises the accumulated features to a probability vector.
func (a *archetypeAcc) posterior(dst *ArchetypePosterior, seen int) *ArchetypePosterior {
	var s [archKindCount]float64
	if a.colors[3] { // R
		s[ArchAggro] += 1.0
		s[ArchBurn] += 1.0
	}
	if a.colors[1] { // U
		s[ArchTempo] += 1.0
		s[ArchControl] += 1.0
	}
	if a.colors[4] { // G
		s[ArchRamp] += 1.0
		s[ArchMidrange] += 0.5
	}
	if a.colors[0] { // W
		s[ArchGoWide] += 1.0
	}
	if a.colors[2] { // B
		s[ArchControl] += 0.5
		s[ArchBurn] += 0.5
		s[ArchMidrange] += 0.5
	}
	if a.creatures > 0 {
		avg := float64(a.mvSum) / float64(a.creatures)
		switch {
		case avg <= 2.5:
			s[ArchAggro] += 1.5
		case avg >= 4.0:
			s[ArchMidrange] += 0.8
		default:
			s[ArchMidrange] += 0.4
		}
		if a.fastBodies*2 >= a.creatures {
			s[ArchAggro] += 1.0
		}
	}
	if a.burnFace > 0 {
		s[ArchBurn] += 1.5 * float64(a.burnFace)
	}
	if a.counters > 0 {
		s[ArchTempo] += 1.5 * float64(a.counters)
		s[ArchControl] += 0.8 * float64(a.counters)
	}
	if a.evasion > 0 {
		s[ArchTempo] += 0.6 * float64(a.evasion)
	}
	if a.removal > 0 {
		s[ArchControl] += 0.9 * float64(a.removal)
	}
	if a.flow > 0 {
		s[ArchControl] += 0.4 * float64(a.flow)
	}
	if a.manaSources > 0 {
		s[ArchRamp] += 1.2 * float64(a.manaSources)
	}
	if a.artifacts > 0 {
		s[ArchRamp] += 0.5 * float64(a.artifacts)
	}
	if a.tokens > 0 {
		s[ArchGoWide] += 1.4 * float64(a.tokens)
	}
	if a.smallBodies >= 3 {
		s[ArchGoWide] += 0.8 * float64(a.smallBodies-2)
	}

	sum := 0.0
	for i := 0; i < archKindCount; i++ {
		if s[i] < 0 {
			s[i] = 0
		}
		sum += s[i]
	}
	if sum <= 0 {
		// Cards were revealed but every signal was neutral (vanilla
		// colourless bodies with no class): Known stays false rather than
		// asserting a uniform posterior the evidence does not support.
		return nil
	}
	// dst's struct and map are reused when given (ProjectInto); every
	// field is rewritten and every key of the map is set below.
	p := dst
	if p == nil {
		p = new(ArchetypePosterior)
	}
	scores := p.Scores
	if scores == nil {
		scores = make(map[string]float64, archKindCount)
	} else {
		clear(scores)
	}
	*p = ArchetypePosterior{Known: true, Seen: seen, Scores: scores}
	for i := 0; i < archKindCount; i++ {
		v := s[i] / sum
		p.Scores[archetypeNames[i]] = v
		if v > p.TopScore {
			p.Top, p.TopScore = archetypeNames[i], v
		}
	}
	return p
}
