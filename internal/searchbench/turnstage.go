package searchbench

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// StageActions inserts the source row's land/cast actions at the next legal
// priority windows for actor. Other asks are bridged by the redacted fallback;
// Bridges is therefore a fidelity measurement, not a hidden default.
// RecordedAction retains the source classification alongside the native
// intent. Aggregate 17lands rows do not carry an exact decision timeline, so
// this is the only lossless place to retain which staged source action made a
// root exist.
type RecordedAction struct {
	Kind, Card string
	Intent     decision.Intent
}

// ActionObserver receives an owned native root immediately before a recorded
// action is applied. It may retain the engine clone for search; mutations to
// it cannot affect reconstruction.
type ActionObserver func(root *rules.Engine, d *decision.Decision, action RecordedAction) error

func StageActions(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, a ResolvedActions) (bridges int, err error) {
	return stageActions(e, bots, actor, a, nil)
}

func stageActions(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, a ResolvedActions, observe ActionObserver) (bridges int, err error) {
	for i, card := range a.Lands {
		if bridges, err = stageOne(e, bots, actor, "Play ", card, bridges, observe); err != nil {
			return bridges, fmt.Errorf("land %d: %w", i, err)
		}
	}
	for kind, cards := range [][]string{a.Creatures, a.NonCreatures, a.Instants} {
		for i, card := range cards {
			if bridges, err = stageOne(e, bots, actor, "Cast ", card, bridges, observe); err != nil {
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
	return stageGame(e, bots, g, nil)
}

// StageGameObserve is StageGame with root capture for each recorded card
// action. It leaves the source engine only with source actions and explicit
// bridges; the observer sees a detached clone before each action.
func StageGameObserve(e *rules.Engine, bots [2]*seat.Bot, g ResolvedReplayGame, observe ActionObserver) (bridges int, err error) {
	if observe == nil {
		return 0, fmt.Errorf("searchbench: nil action observer")
	}
	return stageGame(e, bots, g, observe)
}

func stageGame(e *rules.Engine, bots [2]*seat.Bot, g ResolvedReplayGame, observe ActionObserver) (bridges int, err error) {
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
			if n, err = stageActions(e, bots, side.actor, side.a, observe); err != nil {
				return bridges + n, fmt.Errorf("turn %d %s: %w", turn.Number, side.name, err)
			}
			bridges += n
			if n, err = stageAttackers(e, bots, side.actor, side.a.Attackers, observe); err != nil {
				return bridges + n, fmt.Errorf("turn %d %s attackers: %w", turn.Number, side.name, err)
			}
			bridges += n
		}
	}
	return bridges, nil
}

func stageAttackers(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, names []string, observe ActionObserver) (int, error) {
	for bridges := 0; bridges < 128; bridges++ {
		if err := e.AdvanceHypothetical(); err != nil {
			return bridges, err
		}
		d := e.Pending()
		if d == nil {
			return bridges, fmt.Errorf("game ended before attack declaration")
		}
		if d.Player == actor && d.Kind == decision.KAttackers {
			used, picks := make([]bool, len(d.Options)), make([]int, 0, len(names))
			for _, name := range names {
				found := -1
				for i, o := range d.Options {
					if !used[i] && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face().Name == name {
						found = i
						break
					}
				}
				if found < 0 {
					return bridges, fmt.Errorf("attacker %q is not offered", name)
				}
				used[found] = true
				picks = append(picks, d.Options[found].Index)
			}
			sort.Ints(picks)
			in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}
			if observe != nil {
				if err := observe(e.Clone(), d.Clone(), RecordedAction{Kind: "attack", Card: strings.Join(names, "|"), Intent: in}); err != nil {
					return bridges, err
				}
			}
			return bridges, e.SubmitHypothetical(in)
		}
		p := d.Player
		if int(p) >= len(bots) || bots[p] == nil {
			return bridges, fmt.Errorf("no fallback for player %d", p)
		}
		if err := SubmitBridge(e, bots[p], p); err != nil {
			return bridges, err
		}
	}
	return 128, fmt.Errorf("no attack declaration")
}

func stageOne(e *rules.Engine, bots [2]*seat.Bot, actor state.PlayerID, verb, card string, bridges int, observe ActionObserver) (int, error) {
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
				if observe != nil {
					if err := observe(e.Clone(), d.Clone(), RecordedAction{Kind: actionKind(verb), Card: card, Intent: decision.CloneIntent(in)}); err != nil {
						return bridges, err
					}
				}
				return bridges, e.SubmitHypothetical(in)
			}
			if verb == "Cast " {
				if in, paymentErr := NamedPayment(e, d, card); paymentErr == nil {
					if observe != nil {
						if err := observe(e.Clone(), d.Clone(), RecordedAction{Kind: actionKind(verb), Card: card, Intent: decision.CloneIntent(in)}); err != nil {
							return bridges, err
						}
					}
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

func actionKind(verb string) string {
	if verb == "Play " {
		return "land"
	}
	return "spell"
}
