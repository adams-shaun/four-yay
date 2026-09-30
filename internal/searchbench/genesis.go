package searchbench

import (
	"fmt"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// NewGenesis builds a genuine hypothetical Gorge engine whose player-zero
// opening hand is the recorded public 17lands hand. The opponent deck is an
// explicit belief-world input; it is never taken from hidden source data.
func NewGenesis(reg *cards.Registry, game ReplayGame, cardNames map[string]string, opponent []*cards.Card, seed uint64) (*rules.Engine, error) {
	user, err := ResolveDeck(reg, game.Deck)
	if err != nil {
		return nil, err
	}
	if len(opponent) < 40 {
		return nil, fmt.Errorf("searchbench: opponent belief deck has %d cards", len(opponent))
	}
	hand, err := ResolveEvidence(game.OpeningHand, cardNames)
	if err != nil {
		return nil, err
	}
	want := make(map[string]int, len(hand))
	for _, name := range hand {
		want[name]++
	}
	planner := func(ctx rules.ShuffleContext) ([]state.ObjID, error) {
		if ctx.Player != 0 || ctx.Ordinal != 0 {
			return nil, nil
		}
		out := make([]state.ObjID, 0, len(ctx.Library))
		used := make([]bool, len(ctx.Library))
		for _, name := range hand {
			found := -1
			for i, card := range ctx.Library {
				if !used[i] && card.Name == name {
					found = i
					break
				}
			}
			if found < 0 {
				return nil, fmt.Errorf("searchbench: recorded opening card %q is not in deck", name)
			}
			used[found] = true
			out = append(out, ctx.Library[found].ID)
		}
		for i, card := range ctx.Library {
			if !used[i] {
				out = append(out, card.ID)
			}
		}
		if len(out) != len(ctx.Library) || len(want) == 0 {
			return nil, fmt.Errorf("searchbench: invalid opening-hand permutation")
		}
		return out, nil
	}
	e, err := rules.NewHypotheticalPlanned(rules.Config{Seed: seed, Names: []string{"human", "belief"}, Decks: [][]*cards.Card{user, opponent}, Tokens: reg.Tokens}, nil, planner)
	if err != nil {
		return nil, err
	}
	return e, nil
}
