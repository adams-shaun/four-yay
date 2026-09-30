package searchbench

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
)

// ResolveDeck converts the public deck_* columns into the exact card pointers
// rules.Config needs. Any missing printing rejects the game before staging.
func ResolveDeck(reg *cards.Registry, deck []DeckCard) ([]*cards.Card, error) {
	if reg == nil {
		return nil, fmt.Errorf("searchbench: nil card registry")
	}
	var out []*cards.Card
	for _, entry := range deck {
		card, ok := reg.Lookup(entry.Name)
		if !ok {
			return nil, fmt.Errorf("searchbench: deck card %q is absent from the Forge corpus", entry.Name)
		}
		for range entry.Count {
			out = append(out, card)
		}
	}
	if len(out) < 40 {
		return nil, fmt.Errorf("searchbench: deck has %d cards, want at least 40", len(out))
	}
	return out, nil
}
