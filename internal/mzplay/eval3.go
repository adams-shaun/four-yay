package mzplay

import (
	"math"

	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Port of GameStateEvaluator3.java (mage/player/ai/score, XMage fork
// 48e4918413): upstream's "offline" value function, the leaf of generation
// 0's search and of the evaluation baseline (mcts.offline_mode).
//
//	score(p) = 0.6 * max(0, life)
//	         + H(cards in hand)              H(n) = 1 + 1/2 + ... + 1/n
//	         + sum over p's permanents of (1 + mana value)
//	value    = tanh((score(me) - score(opponent)) / 15)
//
// The Java constants: LIFE_TOTAL_VALUE 0.6, HAND_CARD_VALUE 1.0,
// PERM_BASE_VALUE 1.0, UNTAPPED_BONUS 0, NORMALIZATION_FACTOR 15. The
// permanents are Battlefield.getAllActivePermanents(playerId): controlled
// by the player and phased in, tokens and lands included. A permanent's
// mana value is its current face's (a land, an ordinary token and a
// face-down permanent are 0; a copy has the copied card's).

const (
	eval3LifeValue = 0.6
	eval3HandValue = 1.0
	eval3PermBase  = 1.0
	eval3Normalise = 15.0
)

// Evaluator3 is GameStateEvaluator3.evaluateNormalized for player p: a value
// in [-1, 1]. A finished game is -1 for the loser and +1 for the winner; a
// finished game nobody won falls through to the resource formula, as the
// Java does.
func Evaluator3(e *rules.Engine, p state.PlayerID) float64 {
	g := e.G
	if len(g.Players) != 2 || int(p) >= 2 {
		return 1 // "opponentId == null": no opponent to compare against
	}
	if g.Over && !g.Draw && int(g.Winner) < len(g.Players) {
		if g.Winner == p {
			return 1
		}
		return -1
	}
	return math.Tanh((eval3Resources(e, p) - eval3Resources(e, 1-p)) / eval3Normalise)
}

func eval3Resources(e *rules.Engine, p state.PlayerID) float64 {
	g := e.G
	score := float64(max(0, g.Players[p].Life)) * eval3LifeValue
	score += harmonic(len(g.Zone(state.ZHand, p))) * eval3HandValue
	for i := range g.Players {
		for _, id := range g.Zone(state.ZBattlefield, g.Players[i].ID) {
			o := g.Obj(id)
			if o == nil || o.Controller != p || o.PhasedOut || o.Face() == nil {
				continue
			}
			score += eval3PermBase
			if !o.FaceDown {
				score += float64(o.Face().Cmc())
			}
		}
	}
	return score
}

func harmonic(n int) float64 {
	sum := 0.0
	for i := 1; i <= n; i++ {
		sum += 1.0 / float64(i)
	}
	return sum
}

// OfflineLeaf is Evaluator3 as an azmcts leaf: the searching seat's value
// mapped from [-1, 1] onto azmcts's [0, 1] by v01 = (v + 1) / 2.
func OfflineLeaf(e *rules.Engine, actor state.PlayerID) float64 {
	return (Evaluator3(e, actor) + 1) / 2
}
