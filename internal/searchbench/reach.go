package searchbench

import (
	"context"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchbench/statespec"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// ReachOptions says which decision an item is.
type ReachOptions struct {
	// DecisionPlayer is the seat whose decision the item is (upstream's
	// decisionPlayer, always A for 17lands items).
	DecisionPlayer state.PlayerID
	// Turn is the global turn of the decision (upstream's decideFrom.turn).
	Turn int32
	// PreLand is the land the decision player plays first, at its first
	// main-phase priority of Turn: a card name or its "Play <name>" key.
	PreLand string
	// BotSeed seeds the redacted bot that answers intervening asks.
	BotSeed uint64
	// MaxDecisions bounds the walk (0: 1000).
	MaxDecisions int
}

// ItemReachOptions derives an item's ReachOptions from its spec labels, as
// upstream's make_item does: labels.bridge's decision player and decideFrom
// turn, and (for every kind but block) the first recorded land as preLand.
func ItemReachOptions(spec *statespec.Spec, kind DecisionType, seed uint64) (ReachOptions, error) {
	l, err := spec.ParseLabels()
	if err != nil {
		return ReachOptions{}, refuse("labels", "%v", err)
	}
	o := ReachOptions{DecisionPlayer: SeatID("A"), BotSeed: seed}
	if l.Bridge.DecisionPlayer != "" {
		o.DecisionPlayer = SeatID(l.Bridge.DecisionPlayer)
	}
	if l.Bridge.DecideFrom == nil {
		return o, refuse("labels", "no bridge.decideFrom")
	}
	o.Turn = int32(l.Bridge.DecideFrom.Turn)
	if kind != DecisionBlock {
		if lands := l.KeyedLands(); len(lands) > 0 {
			o.PreLand = lands[0].Key
		}
	}
	return o, nil
}

// Reached is the item decision Reach stopped at.
type Reached struct {
	Kind     DecisionType
	Decision *decision.Decision
	// BotAnswers counts the intervening non-priority asks the redacted bot
	// answered; Passes the intervening priority passes; Declined the
	// intervening attack/block declarations answered with "none" (the
	// upstream puppet's answer).
	BotAnswers, Passes, Declined int
	PreLandPlayed                bool
}

// Reach advances e from its staged point to the item's decision:
//
//   - spell, hold: the decision player's priority in the precombat main
//     phase of Turn with an empty stack, after the preLand is played (the
//     "Play <name>" priority option; refused as "preland" when not offered);
//   - attack: the decision player's KAttackers decision in Turn (preLand
//     played at its main-phase priority first);
//   - block: the decision player's KBlockers decision in Turn. Blockers are
//     declared as the declare-blockers step begins, so a priority in that
//     step means none was asked ("reached PRIORITY at DECLARE_BLOCKERS", as
//     upstream's bridge reports it).
//
// On the way, every priority is passed, every other attack or block
// declaration is answered with none (upstream's puppet: it never attacks or
// blocks, and the decider acts like it before decideFrom), and every other
// ask is answered by the redacted deterministic seat.Bot. A walk past the
// decision is a refusal: "wrong turn" when the turn moves past Turn,
// "reached" (Detail "<kind> at <STEP>") when Turn's decision comes at the
// wrong kind or step, "game over" when the game ends.
func Reach(e *rules.Engine, kind DecisionType, o ReachOptions) (*Reached, error) {
	bot := seat.NewBot(o.BotSeed)
	limit := o.MaxDecisions
	if limit <= 0 {
		limit = 1000
	}
	preLand := strings.TrimPrefix(o.PreLand, "Play ")
	r := &Reached{Kind: kind}
	dp := o.DecisionPlayer
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			if err := e.AdvanceHypothetical(); err != nil {
				return r, refuse("chance", "%v", err)
			}
			if d = e.Pending(); d == nil {
				if e.G.Over {
					return r, refuse("game over", "turn %d", e.G.Turn)
				}
				return r, refuse("no decision", "turn %d %s", e.G.Turn, StepName(e.G.Step))
			}
		}
		if e.G.Over {
			return r, refuse("game over", "turn %d", e.G.Turn)
		}
		if e.G.Turn > o.Turn {
			return r, refuse("wrong turn", "turn %d, the item is turn %d", e.G.Turn, o.Turn)
		}
		inTurn := e.G.Turn == o.Turn
		var in decision.Intent
		switch {
		case d.Kind == decision.KPriority:
			if inTurn && d.Player == dp && e.G.Step == state.StepMain1 && len(e.G.Stack) == 0 &&
				e.G.Active == dp && (kind == DecisionSpell || kind == DecisionHold || kind == DecisionAttack) {
				if preLand != "" && !r.PreLandPlayed {
					idx := -1
					for _, opt := range d.Options {
						if opt.Kind == "play_land" && objName(e, opt.Obj) == preLand {
							idx = opt.Index
							break
						}
					}
					if idx < 0 {
						return r, refuse("preland", "Play %s is not offered", preLand)
					}
					in = decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
					r.PreLandPlayed = true
					break
				}
				if kind != DecisionAttack {
					r.Decision = d
					return r, nil
				}
			}
			if inTurn && (kind == DecisionAttack && e.G.Active == dp && e.G.Step > state.StepDeclareAttackers ||
				kind == DecisionBlock && e.G.Step >= state.StepDeclareBlockers) {
				return r, refuse("reached", "%s at %s", strings.ToUpper(string(d.Kind)), StepName(e.G.Step))
			}
			in = passIntent(d)
			r.Passes++
		case d.Kind == decision.KAttackers:
			if inTurn && d.Player == dp && kind == DecisionAttack {
				r.Decision = d
				return r, nil
			}
			if inTurn && kind == DecisionBlock {
				return r, refuse("reached", "%s at %s", strings.ToUpper(string(d.Kind)), StepName(e.G.Step))
			}
			in = decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
			r.Declined++
		case d.Kind == decision.KBlockers:
			if inTurn && d.Player == dp && kind == DecisionBlock {
				r.Decision = d
				return r, nil
			}
			in = decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}
			r.Declined++
		default:
			v := view.Project(e.G, e, d.Player, d)
			v.Round = view.RoundOf(e.G, e.L.Events)
			var err error
			if in, err = bot.Decide(context.Background(), v, *d); err != nil {
				return r, refuse("bot", "%v", err)
			}
			r.BotAnswers++
		}
		if err := e.SubmitHypothetical(in); err != nil {
			return r, refuse("submit", "%s: %v", d.Kind, err)
		}
	}
	return r, refuse("no decision", "walk limit %d", limit)
}

func passIntent(d *decision.Decision) decision.Intent {
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}
		}
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
}

// objName is an object's current face name ("" for none).
func objName(e *rules.Engine, id state.ObjID) string {
	if o := e.G.Obj(id); o != nil && o.Face() != nil {
		return o.Face().Name
	}
	return ""
}
