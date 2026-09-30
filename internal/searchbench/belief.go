package searchbench

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
)

// ObservedOpponentDeck makes a legal 40-card belief deck without treating the
// opponent's private deck as source data. It replaces cards in base with every
// public opponent land or spell observed in the replay. The remaining cards
// are deliberately only a filler prior; callers must label this construction
// as an observed-card belief world, not as a recovered opponent deck.
func ObservedOpponentDeck(reg *cards.Registry, base []*cards.Card, g ReplayGame, names map[string]string) ([]*cards.Card, error) {
	if reg == nil {
		return nil, fmt.Errorf("searchbench: nil card registry")
	}
	if len(base) < 40 {
		return nil, fmt.Errorf("searchbench: base belief deck has %d cards, want at least 40", len(base))
	}
	var observed []*cards.Card
	for _, turn := range g.Turns {
		for _, ids := range [][]string{turn.Opponent.Lands, turn.Opponent.Creatures, turn.Opponent.NonCreatures, turn.Opponent.Instants} {
			for _, id := range ids {
				name, ok := names[id]
				if !ok || name == "" {
					return nil, fmt.Errorf("searchbench: opponent card id %q has no printed name", id)
				}
				card, ok := reg.Lookup(name)
				if !ok {
					return nil, fmt.Errorf("searchbench: observed opponent card %q is absent from the Forge corpus", name)
				}
				observed = append(observed, card)
			}
		}
	}
	if len(observed) > len(base) {
		return nil, fmt.Errorf("searchbench: opponent has %d observed deck cards, base has %d", len(observed), len(base))
	}
	out := append([]*cards.Card(nil), base...)
	// The source traversal above is stable. Replacing from the end keeps the
	// prior's leading order intact before NewGenesis applies its explicit draw
	// plan.
	for i, card := range observed {
		out[len(out)-1-i] = card
	}
	return out, nil
}

// ObservedOpponentDrawPlan is the public sequence that the belief world's
// initial shuffle must make available. It contains only lands and spells that
// the source says the opponent subsequently revealed by playing or casting.
func ObservedOpponentDrawPlan(g ReplayGame, names map[string]string) ([]string, error) {
	var out []string
	for _, turn := range g.Turns {
		for _, ids := range [][]string{turn.Opponent.Lands, turn.Opponent.Creatures, turn.Opponent.NonCreatures, turn.Opponent.Instants} {
			for _, id := range ids {
				name, ok := names[id]
				if !ok || name == "" {
					return nil, fmt.Errorf("searchbench: opponent card id %q has no printed name", id)
				}
				out = append(out, name)
			}
		}
	}
	return out, nil
}
