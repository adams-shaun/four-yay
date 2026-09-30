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
	for _, card := range a.Lands {
		if bridges, err = stageOne(e, bots, actor, "Play ", card, bridges); err != nil {
			return bridges, err
		}
	}
	for _, cards := range [][]string{a.Creatures, a.NonCreatures, a.Instants} {
		for _, card := range cards {
			if bridges, err = stageOne(e, bots, actor, "Cast ", card, bridges); err != nil {
				return bridges, err
			}
		}
	}
	return bridges, nil
}

func stageOne(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, verb, card string, bridges int) (int, error) {
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
		}
		p := d.Player
		if int(p) >= len(bots) || bots[p] == nil {
			return bridges, fmt.Errorf("searchbench: no fallback for player %d", p)
		}
		if err := SubmitFallback(e, bots[p], p); err != nil {
			return bridges, err
		}
		bridges++
	}
	return bridges, fmt.Errorf("searchbench: no priority window for %s%s", verb, card)
}
