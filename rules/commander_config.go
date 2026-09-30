package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/deck"
)

// CR 903.3. It DELEGATES to deck.IsCommanderEligible -- the one
// implementation the deck validator (File.ValidateCommander) also uses -- so
// the validator and the engine can never disagree about which cards seat:
// a legendary creature, a legendary Vehicle, a legendary Spacecraft with a
// printed power/toughness box (Hearthhull, the Worldseed), or a card whose
// printed/Oracle text says it can be your commander (the planeswalker
// commanders and the Partner/choose-a-background cases). The old inline
// predicate here (a legendary creature on Faces[0], commander permission
// read from Keywords only) was the stale half of a two-implementation
// disagreement and rejected decks the validator accepted.
func commanderCardLegal(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 {
		return false
	}
	return deck.IsCommanderEligible(c)
}

// partnerPairOK reports whether two cards may be a commander PAIR: each
// carries a Partner-family ability and either both are plain Partners, or
// each "Partner with" the other by printed name (CR 903.13a/c), or at least
// one carries K:Doctor's companion and the other is a Doctor (the Doctor Who
// cycle's companion clause, which also admits two distinct Doctors that each
// carry it). The check
// itself lives in deck.IsPartnerPair — the same package that owns
// IsCommanderEligible (which commanderCardLegal above already delegates to),
// so the deck-file validator and the engine's seating gate cannot disagree
// about what a legal pair is.
func partnerPairOK(a, b *cards.Card) bool {
	return deck.IsPartnerPair(a, b)
}

// legalCommandersFor validates seat i's configured commander list against
// the deck-construction rules (CR 903.4/903.13) and returns the indices
// that MAY be seated, in Config order: a single commander must be a
// legendary creature or a "can be your commander" card; a two-card seat is
// a legal partner pair (plain Partners, a mutual "Partner with" pair, or a
// Doctor's-companion pair);
// anything else -- a noncommander card, a pair without partner, more than
// two -- is rejected WHOLE, never silently trimmed into a legal-looking
// subset. This is what makes an illegal Config fail in play: the rejected
// seat plays commander-less and the rejection is on the log as a Note (the
// same degrade-don't-crash stance New takes for malformed decks elsewhere --
// New cannot return an error, so the Note is the record).
func (c *Config) legalCommandersFor(i, deckLen int, deck []*cards.Card) []int {
	raw := c.commandersFor(i, deckLen)
	if len(raw) == 0 {
		return nil
	}
	// Construction legality belongs to Commander games. Config.Commanders also
	// intentionally powers constructed-format fixture and compatibility paths
	// (where it merely selects command-zone objects), so preserve that legacy
	// plumbing outside FormatCommander.
	if c.Format != FormatCommander {
		return raw
	}
	bad := func(why string) ([]int, string) {
		names := ""
		for _, idx := range raw {
			if idx >= 0 && idx < len(deck) && deck[idx] != nil {
				names += deck[idx].Faces[0].Name + ", "
			}
		}
		return nil, names + why
	}
	reject := ""
	switch len(raw) {
	case 1:
		idx := raw[0]
		if idx >= 0 && idx < len(deck) && !commanderCardLegal(deck[idx]) {
			_, reject = bad("is not a legendary creature and does not say it can be your commander")
		}
	case 2:
		a, b := raw[0], raw[1]
		inRange := func(x int) bool { return x >= 0 && x < len(deck) && deck[x] != nil }
		if !inRange(a) || !inRange(b) {
			_, reject = bad("is not a card this deck carries")
		} else if !commanderCardLegal(deck[a]) || !commanderCardLegal(deck[b]) || !partnerPairOK(deck[a], deck[b]) {
			_, reject = bad("is not a legal commander pair")
		}
	default:
		_, reject = bad("is not one or two commanders")
	}
	if reject != "" {
		return nil
	}
	return raw
}

// commandersFor returns the VALID commander indices (into deck of length
// deckLen) that Config names for seat i, in Config order. An index out of
// range for the deck, or a seat with no Commanders entry, contributes
// nothing -- the same degrade-don't-crash stance New already takes for more
// decks than seats: the bogus entry is skipped, never a panic.
func (c *Config) commandersFor(i, deckLen int) []int {
	if i < 0 || i >= len(c.Commanders) {
		return nil
	}
	var out []int
	for _, idx := range c.Commanders[i] {
		if idx >= 0 && idx < deckLen {
			out = append(out, idx)
		}
	}
	return out
}
