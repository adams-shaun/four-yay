package botpolicy

import (
	"cmp"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/decision"
)

// MulliganRule selects how the policy answers the London mulligan round
// (rules/mulligan.go): the keep/mulligan ask and the bottoming ask. It is
// CONFIGURATION carried on the Board (Board.Mulligan), like Cast: the
// adapters never fill it, and the zero value is the historical answer, byte
// for byte.
type MulliganRule uint8

const (
	// MulliganCoin is the default (the zero value): a keep/mulligan ask
	// mulligans with probability 1/3 off the bot's rng, whatever the hand
	// holds, and bottoming puts back chooseDiscard's least valuable cards.
	MulliganCoin MulliganRule = iota
	// MulliganLands is the opt-in Limited heuristic (40-card decks, ~17
	// lands): keep or mulligan on the hand's land count, and bottom so the
	// kept hand has a sensible land count (landMulligan, landBottoming).
	// It consumes no rng.
	MulliganLands
	// MulliganNever always keeps: the "no mulligan" control for measuring a
	// mulligan rule inside a game that runs the London round. It consumes no
	// rng. (A seat that never mulligans is never asked to bottom in a
	// two-player game; a multiplayer bottoming ask still uses chooseDiscard.)
	MulliganNever
)

// mulliganByRule answers a KMulligan ask under a non-default rule. ok is
// false for MulliganCoin, which decide answers itself.
func (b Board) mulliganByRule(d *decision.Decision) (choices []int, ok bool) {
	if b.Mulligan == MulliganCoin || len(d.Options) == 0 {
		return nil, false
	}
	if d.Options[0].Kind == "bottom" {
		if b.Mulligan == MulliganLands {
			return b.landBottoming(d), true
		}
		return b.chooseDiscard(d), true
	}
	keep, mull := -1, -1
	for _, o := range d.Options {
		switch o.Kind {
		case "keep":
			keep = o.Index
		case "mulligan":
			mull = o.Index
		}
	}
	if keep < 0 {
		return nil, false // not a keep/mulligan shape: the default arm decides
	}
	if mull >= 0 && b.Mulligan == MulliganLands && !b.keepByLands(keepBottomCount(d.Prompt)) {
		return []int{mull}, true
	}
	return []int{keep}, true
}

// isLandCard is the policy's land test for a hand card: a basic, or a
// noncreature with no mana cost (CMC 0 and an empty or "no cost" printed
// cost). Card carries no type line beyond Basic/Creature, so this is the
// same proxy chooseDiscard's "basic or CMC-0 noncreature" class uses,
// narrowed by the printed cost so a {0} artifact is a spell. It is exact
// for the FDN Limited pool (every non-basic land there -- the gain lands,
// Evolving Wilds, Rogue's Passage -- prints "no cost", and no nonland card
// does); a suspend-only card with no mana cost would count as a land.
func isLandCard(c Card) bool {
	if c.Basic {
		return true
	}
	if c.Creature || c.CMC != 0 {
		return false
	}
	mc := strings.TrimSpace(c.ManaCost)
	return mc == "" || strings.EqualFold(mc, "no cost")
}

// keepBottomCount reads the bottoming penalty a keep accepts off a
// keep/mulligan prompt (rules/mulligan.go's keepMulliganPrompt: "... Keep
// your hand (put 1 card on the bottom of your library) or take a
// mulligan?"). The decision carries the count nowhere else; the hand is
// always a full seven while the seat decides (CR 103.4), so the penalty is
// how the policy knows how many mulligans it has taken. A prompt it cannot
// read ("keep all seven cards", a multiplayer free mulligan, or any other
// wording) is 0: the opening-seven rule.
func keepBottomCount(prompt string) int {
	i := strings.LastIndex(prompt, "Keep your hand (")
	if i < 0 {
		return 0
	}
	s := prompt[i+len("Keep your hand ("):]
	if !strings.HasPrefix(s, "put ") {
		return 0
	}
	s = s[len("put "):]
	n, digits := 0, 0
	for digits < len(s) && s[digits] >= '0' && s[digits] <= '9' {
		n = n*10 + int(s[digits]-'0')
		digits++
	}
	if digits == 0 {
		return 0
	}
	return n
}

// handLands counts the deciding seat's hand and the lands in it. During
// the pregame the castable, off-battlefield Cards entries are exactly the
// hand (the graveyard and command zone are empty in Limited); ok is false
// when that census disagrees with HandSize, and the caller then keeps.
func (b Board) handLands() (hand, lands int, ok bool) {
	for _, c := range b.Cards.All() {
		if !c.Castable || c.OnBattlefield {
			continue
		}
		hand++
		if isLandCard(c) {
			lands++
		}
	}
	return hand, lands, hand > 0 && int32(hand) == b.HandSize
}

// keepByLands is MulliganLands' keep/mulligan rule for a hand that will
// bottom `bottom` cards if kept (0 on the opening seven, 1 after one
// London mulligan, ...). With K = hand - bottom cards to keep:
//
//   - K >= 6 (the opening seven, or one mulligan): keep 2 to 5 lands among
//     the seven drawn; mulligan 0, 1, 6 or 7.
//   - K == 5 (two mulligans): keep unless the seven hold no land or no
//     spell.
//   - K <= 4: always keep.
//
// A hand the census cannot read (handLands' ok false) is kept.
func (b Board) keepByLands(bottom int) bool {
	hand, lands, ok := b.handLands()
	if !ok {
		return true
	}
	switch k := hand - bottom; {
	case k >= 6:
		return lands >= 2 && lands <= 5
	case k == 5:
		return lands >= 1 && lands < hand
	default:
		return true
	}
}

// landBottoming is MulliganLands' bottoming answer: d.Max cards go to the
// bottom so the kept hand (K = len(d.Options) - d.Max cards) holds at most
// maxLands = max(ceil(K/2), min(2, K)) lands. Lands go first while the kept
// hand would hold more than that; every other pick is a spell, the highest
// mana value first (ties on the lower option index, chooseDiscard's order
// for spells). Since maxLands is at least 2 for K >= 2, the rule never
// bottoms a hand below two lands while it has them. Among lands, the one
// whose colours the hand's spells need least is bottomed first: each land
// scores, over the colours it produces, the spells' pips of that colour
// divided by how many remaining lands produce it (a land with no recorded
// production, such as Evolving Wilds, or an "Any" producer counts for every
// colour); the lowest score goes, ties on the lower index. The answer is in
// ascending index order, like chooseLowest's.
func (b Board) landBottoming(d *decision.Decision) []int {
	m := d.Max
	n := len(d.Options)
	if m <= 0 {
		return nil
	}
	if m > n {
		m = n
	}
	type handCard struct {
		idx  int
		c    Card
		land bool
	}
	var lands, spells []handCard
	for _, o := range d.Options {
		// A card the table does not hold (never, for a hand card) is a
		// spell: the zero Card would otherwise read as a costless land.
		c, known := b.Cards.Lookup(o.Obj)
		hc := handCard{idx: o.Index, c: c, land: known && isLandCard(c)}
		if hc.land {
			lands = append(lands, hc)
		} else {
			spells = append(spells, hc)
		}
	}
	k := n - m
	maxLands := max((k+1)/2, min(2, k))
	landsOut := min(max(len(lands)-maxLands, 0), m)
	spellsOut := m - landsOut
	if spellsOut > len(spells) { // unreachable for maxLands <= k; kept for safety
		spellsOut = len(spells)
		landsOut = m - spellsOut
	}
	out := make([]int, 0, m)

	// Spells: highest mana value first, ties on the lower index.
	ranked := slices.Clone(spells)
	slices.SortStableFunc(ranked, func(a, c handCard) int {
		return cmp.Or(cmp.Compare(c.c.CMC, a.c.CMC), cmp.Compare(a.idx, c.idx))
	})
	keptSpells := ranked[spellsOut:]
	for _, s := range ranked[:spellsOut] {
		out = append(out, s.idx)
	}

	// Lands: greedily drop the least needed one, recounting cover each time.
	var need [5]int32
	for _, s := range keptSpells {
		p := colourPips(s.c.ManaCost)
		for i := range need {
			need[i] += p[i]
		}
	}
	produces := func(c Card, colour int) bool {
		pr := c.Produces
		if pr.Any {
			return true
		}
		none := true
		for _, v := range pr.Colour {
			if v != 0 {
				none = false
				break
			}
		}
		return none || pr.Colour[colour] > 0
	}
	left := slices.Clone(lands)
	for drop := 0; drop < landsOut; drop++ {
		var cover [5]int32
		for _, l := range left {
			for col := range cover {
				if produces(l.c, col) {
					cover[col]++
				}
			}
		}
		best, bestScore := -1, 0.0
		for li, l := range left {
			score := 0.0
			for col := range need {
				if need[col] > 0 && produces(l.c, col) {
					score += float64(need[col]) / float64(cover[col])
				}
			}
			if best < 0 || score < bestScore {
				best, bestScore = li, score
			}
		}
		out = append(out, left[best].idx)
		left = slices.Delete(left, best, best+1)
	}
	slices.Sort(out)
	return out
}
