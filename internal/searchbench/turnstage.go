package searchbench

import (
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// StageActions inserts the source row's land/cast actions at the next legal
// priority windows for actor. Other asks are bridged by the redacted fallback;
// Bridges is therefore a fidelity measurement, not a hidden default.
func StageActions(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, a ResolvedActions) (bridges int, err error) {
	for i, card := range a.Lands {
		if bridges, err = stageOne(e, bots, actor, "Play ", card, bridges); err != nil {
			return bridges, fmt.Errorf("land %d: %w", i, err)
		}
	}
	for kind, cards := range [][]string{a.Creatures, a.NonCreatures, a.Instants} {
		for i, card := range cards {
			if bridges, err = stageOne(e, bots, actor, "Cast ", card, bridges); err != nil {
				return bridges, fmt.Errorf("cast group %d card %d: %w", kind, i, err)
			}
		}
	}
	return bridges, nil
}

// StageGame replays the aggregate row in its recorded user/opponent turn
// order. It is intentionally strict about every named action; its caller can
// retain only roots whose preceding history reaches the required fidelity.
func StageGame(e *rules.Engine, bots [2]*seat.Bot, g ResolvedReplayGame) (bridges int, err error) {
	for _, turn := range g.Turns {
		order := []struct {
			actor state.PlayerID
			name  string
			a     ResolvedActions
		}{{0, "user", turn.User}, {1, "opponent", turn.Opponent}}
		if !g.OnPlay {
			order[0], order[1] = order[1], order[0]
		}
		for _, side := range order {
			var n int
			if n, err = StageActions(e, bots, side.actor, side.a); err != nil {
				return bridges + n, fmt.Errorf("turn %d %s: %w", turn.Number, side.name, err)
			}
			bridges += n
		}
	}
	return bridges, nil
}

func stageOne(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, verb, card string, bridges int) (int, error) {
	lastUnavailable := ""
	for steps := 0; steps < 128; steps++ {
		if err := e.AdvanceHypothetical(); err != nil {
			return bridges, err
		}
		d := e.Pending()
		if d == nil {
			return bridges, fmt.Errorf("searchbench: game ended before %s%s", verb, card)
		}
		if d.Player == actor && d.Kind == decision.KPriority {
			in, err := NamedOption(d, verb, card)
			if err == nil {
				return bridges, e.SubmitHypothetical(in)
			}
			if verb == "Cast " {
				if in, paymentErr := NamedPayment(e, d, card); paymentErr == nil {
					return bridges, e.SubmitHypothetical(in)
				}
			}
			lastUnavailable = err.Error()
		}
		p := d.Player
		if int(p) >= len(bots) || bots[p] == nil {
			return bridges, fmt.Errorf("searchbench: no fallback for player %d", p)
		}
		if err := SubmitBridge(e, bots[p], p); err != nil {
			return bridges, err
		}
		bridges++
	}
	if lastUnavailable != "" {
		return bridges, fmt.Errorf("searchbench: %s", lastUnavailable)
	}
	return bridges, fmt.Errorf("searchbench: no priority window for %s%s", verb, card)
}
