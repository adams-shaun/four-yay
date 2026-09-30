package searchbench

import "fmt"

// ResolvedReplayGame is ReplayGame with every card-valued Arena id translated
// to its printed name. Activated ability ids remain separate, because treating
// an ability id as a card is a replay-corrupting guess.
type ResolvedReplayGame struct {
	ReplayGame
	OpeningHand []string
	Turns       []ResolvedTurn
}

type ResolvedTurn struct {
	Number         int
	User, Opponent ResolvedActions
}
type ResolvedActions struct {
	Lands, Creatures, NonCreatures, Instants, Attackers, Blockers []string
	Abilities                                                     []string
}

func ResolveReplayGame(g ReplayGame, cards map[string]string) (ResolvedReplayGame, error) {
	hand, err := ResolveEvidence(g.OpeningHand, cards)
	if err != nil {
		return ResolvedReplayGame{}, fmt.Errorf("searchbench: game %s opening hand: %w", g.ID, err)
	}
	out := ResolvedReplayGame{ReplayGame: g, OpeningHand: hand}
	for _, turn := range g.Turns {
		u, err := resolveActions(turn.User, cards)
		if err != nil {
			return ResolvedReplayGame{}, fmt.Errorf("searchbench: game %s turn %d user: %w", g.ID, turn.Number, err)
		}
		o, err := resolveActions(turn.Opponent, cards)
		if err != nil {
			return ResolvedReplayGame{}, fmt.Errorf("searchbench: game %s turn %d opponent: %w", g.ID, turn.Number, err)
		}
		out.Turns = append(out.Turns, ResolvedTurn{Number: turn.Number, User: u, Opponent: o})
	}
	return out, nil
}

func resolveActions(a TurnActions, cards map[string]string) (ResolvedActions, error) {
	resolve := func(ids []string) ([]string, error) { return ResolveEvidence(ids, cards) }
	lands, err := resolve(a.Lands)
	if err != nil {
		return ResolvedActions{}, err
	}
	creatures, err := resolve(a.Creatures)
	if err != nil {
		return ResolvedActions{}, err
	}
	nonCreatures, err := resolve(a.NonCreatures)
	if err != nil {
		return ResolvedActions{}, err
	}
	instants, err := resolve(a.Instants)
	if err != nil {
		return ResolvedActions{}, err
	}
	attackers, err := resolve(a.Attackers)
	if err != nil {
		return ResolvedActions{}, err
	}
	blockers, err := resolve(a.Blockers)
	if err != nil {
		return ResolvedActions{}, err
	}
	return ResolvedActions{Lands: lands, Creatures: creatures, NonCreatures: nonCreatures, Instants: instants, Attackers: attackers, Blockers: blockers, Abilities: append([]string(nil), a.Abilities...)}, nil
}
