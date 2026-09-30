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
	opponentPlan, err := ObservedOpponentDrawPlan(game, cardNames)
	if err != nil {
		return nil, err
	}
	planner := func(ctx rules.ShuffleContext) ([]state.ObjID, error) {
		if ctx.Ordinal != 0 {
			return nil, nil
		}
		var want []string
		switch ctx.Player {
		case 0:
			want = hand
		case 1:
			want = opponentPlan
		default:
			return nil, nil
		}
		picked := make([]state.ObjID, 0, len(want))
		used := make([]bool, len(ctx.Library))
		for _, name := range want {
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
			picked = append(picked, ctx.Library[found].ID)
		}
		// The engine deals from the front of a library slice (pinned by the
		// shuffle planner contract), so the recorded draw sequence is first.
		out := append([]state.ObjID(nil), picked...)
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
	starter := 1
	if game.OnPlay {
		starter = 0
	}
	e, err := rules.NewHypotheticalPlanned(rules.Config{Seed: seed, Names: []string{"human", "belief"}, Decks: [][]*cards.Card{user, opponent}, Tokens: reg.Tokens}, []rules.ChanceDraw{{Bound: 2, Value: starter}}, planner)
	if err != nil {
		return nil, err
	}
	return e, nil
}
