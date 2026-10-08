package cards

import (
	"sort"
	"strconv"
)

// CoverageGroup is one row of a grouped coverage table: how many cards fall
// into the group and how many of those the engine can fully play.
type CoverageGroup struct {
	Key       string
	Cards     int
	Supported int
}

// Percent is the group's playable share, 0 for an empty group (rather than
// NaN, which no report should ever print).
func (g CoverageGroup) Percent() float64 {
	if g.Cards == 0 {
		return 0
	}
	return 100 * float64(g.Supported) / float64(g.Cards)
}

// GroupKey maps a card to the row it belongs in. A key func must be a pure
// function of the card's compiled data: the breakdown is committed to the
// repository, so two runs over the same corpus pin must produce byte-identical
// output.
type GroupKey func(*Card) string

// CoverageBy is Coverage split into rows. It applies exactly the same
// supported/unsupported verdict as Coverage -- a card counts as playable when
// Unsupported returns nothing -- and skips the same unnamed cards, so the row
// totals always add up to Coverage's Cards and Supported.
//
// Rows come back sorted by Key so the output is stable run to run; a caller
// wanting count order re-sorts the returned slice.
func (r *Registry) CoverageBy(supported map[string]bool, key GroupKey) []CoverageGroup {
	idx := map[string]int{}
	var out []CoverageGroup
	for _, c := range r.AllCards() {
		if !c.named() {
			continue
		}
		k := key(c)
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, CoverageGroup{Key: k})
		}
		out[i].Cards++
		if len(r.Unsupported(c, supported)) == 0 {
			out[i].Supported++
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// typeGroupOrder is the precedence the primary-type grouping applies to a
// card printing several card types. An Artifact Creature is reported as a
// Creature, a Land Creature (Dryad Arbor) as a Creature, and an Artifact Land
// as a Land: the more specific behaviour the engine has to implement wins,
// which is what a coverage table is measuring.
var typeGroupOrder = []string{
	"Creature", "Planeswalker", "Battle", "Land",
	"Instant", "Sorcery", "Artifact", "Enchantment",
}

// PrimaryTypeGroup buckets a card by its card type. The first face decides:
// a transforming card's back face is not separately deckable, and a split
// card's halves are near-always the same type. A card printing none of the
// known card types (a scheme, a plane, a dungeon) lands in "Other".
func PrimaryTypeGroup(c *Card) string {
	if c == nil || len(c.Faces) == 0 {
		return "Other"
	}
	f := c.Faces[0]
	for _, t := range typeGroupOrder {
		if f.hasType(t) {
			return t
		}
	}
	return "Other"
}

// colourGroupNames maps a single-colour mask to its printed colour name.
var colourGroupNames = map[uint8]string{
	ColourWhite: "White",
	ColourBlue:  "Blue",
	ColourBlack: "Black",
	ColourRed:   "Red",
	ColourGreen: "Green",
}

// ColourGroup buckets a card by its printed COLOUR -- the colours of its mana
// cost plus its colour indicator, unioned over every face. It is deliberately
// NOT ColourIdentity: identity also folds in the mana symbols in rules text
// (CR 903.4), so a colourless artifact with a {R} activation has red identity
// while the card a player sees is colourless. A coverage table reads better
// split by what the card is than by what a Commander deck may run it in.
func ColourGroup(c *Card) string {
	var m uint8
	if c != nil {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			m |= manaColours(f.ManaCost)
			m |= colourIndicator(f.Colors)
		}
	}
	switch {
	case m == 0:
		return "Colorless"
	case m&(m-1) != 0:
		return "Multicolour"
	default:
		return colourGroupNames[m]
	}
}

// maxManaValueBand is the last band that gets a row of its own; everything at
// or above it shares the "7+" row. Above 6 the corpus thins out fast and a
// per-value row is noise.
const maxManaValueBand = 7

// ManaValueGroup buckets a card by the mana value of its first face. The keys
// sort lexically into numeric order ("0".."6", then "7+"), which is the order
// CoverageBy returns them in.
func ManaValueGroup(c *Card) string {
	if c == nil || len(c.Faces) == 0 {
		return "0"
	}
	mv := int(c.Faces[0].Cmc())
	if mv >= maxManaValueBand {
		return strconv.Itoa(maxManaValueBand) + "+"
	}
	if mv < 0 {
		mv = 0
	}
	return strconv.Itoa(mv)
}
