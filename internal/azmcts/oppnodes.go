package azmcts

// Opponent nodes (Options.OpponentNodes): upstream MageZero searches both
// seats' decisions inside one tree and values the opponent's nodes from the
// opponent's side. With the switch on, a decision of the other seat that is
// a searched kind with at least two candidates becomes a tree point marked
// opp instead of being answered by the bot; selectEdge reads Q in the
// mover's frame there (1 - q), while every stored value -- the leaf, the
// backup, RootValue, the root's Q -- stays the searching seat's win
// probability. Off, nothing here runs and the search is byte-identical to
// one before opponent nodes existed (TestOpponentNodesOffIsByteIdentical).
//
// The candidate generator is already seat-generic (it reads d.Player); only
// the observer is seat-bound, so the opponent gets its own collector, which
// captures the root engine once per Search (its Introduced count is the
// opponent's NameKeys boundary) and is cloned into every world. Under
// AutoPayment the opponent also gets its own auto-pay bot, the mirror of
// the actor's, so its candidate 0 at a priority is a planned cast or a pass
// rather than a manual mana tap outside the vocabulary; that bot then
// answers every opponent decision the tree does not branch on.
//
// What is refused (an error from Search, NewSeat or Validate): more or fewer
// than two players; a world source whose worlds differ between simulations
// (the redeal source, IS-MCTS, a per-simulation chance seed), since an
// opponent node's statistics must describe one world; and RootPerWorld.

import (
	"errors"
	"fmt"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
)

// frame is the seat whose win probability every value is: the searching
// seat, or the opponent under the test-only swapFrame.
func (c *walkConfig) frame() state.PlayerID {
	if c.swapFrame {
		return c.opp
	}
	return c.actor
}

// checkOpponentGame is Search's refusal of a game opponent nodes cannot
// search: anything but two players.
func checkOpponentGame(root Root) error {
	if n := len(root.Engine.G.Players); n != 2 {
		return fmt.Errorf("azmcts: opponent nodes need a two-player game, this one has %d players", n)
	}
	return nil
}

// checkOpponentSource is Search's refusal of a world source opponent nodes
// cannot search: one whose worlds differ between simulations.
func checkOpponentSource(src WorldSource) error {
	if !isFixed(src) {
		return errors.New("azmcts: opponent nodes need a fixed world source (the clairvoyant clone or FixedChance); a redeal, IS-MCTS or per-simulation chance source differs between simulations")
	}
	return nil
}

// prepareOpponent fills cfg's opponent half: the other seat, its candidate
// cap, and its root observer -- a fresh, unchained collector that captures
// the root engine once (every world of a fixed source is a copy of that
// position), whose Introduced count is the opponent's NameKeys boundary.
func prepareOpponent(cfg *walkConfig, root Root, opts Options) error {
	cfg.oppNodes, cfg.swapFrame = true, opts.swapFrame
	cfg.opp = 1 - root.Decision.Player
	cfg.oppLimit = opts.oppLimit()
	obs := searchprobe.NewCollector(cfg.opp)
	obs.Unchain()
	if _, err := obs.Capture(root.Engine, nil); err != nil {
		return fmt.Errorf("azmcts: opponent nodes: capturing the root for seat %d: %w", cfg.opp, err)
	}
	cfg.oppObs, cfg.oppRootRefs = obs, obs.Introduced()
	return nil
}

// initOpponent gives a new world's walker its opponent half: a clone of the
// opponent's root observer and, under AutoPayment, the opponent's auto-pay
// bot, seeded identically for every simulation like the actor's.
func (e *engineEnv) initOpponent() {
	e.oppObs = e.cfg.oppObs.Clone()
	if e.cfg.autoPayment {
		e.oppPCG = seat.BotSource(splitmix(e.cfg.envSeed ^ 0x6f70702d61702d62 ^ uint64(e.cfg.opp)))
		e.oppBot = seat.NewBotOn(e.oppPCG).EnableAutoPayMana()
	}
}

// oppPoint is the opponent's searched decision pd as a tree point, bot (the
// opponent bot's answer) first, or false when pd is not one (not a searched
// kind, fewer than two candidates, a bot answer outside the vocabulary): the
// bot's answer is then played, exactly as for the actor.
func (e *engineEnv) oppPoint(pd *decision.Decision, bot decision.Intent) (*Point, bool) {
	cands, kind, _, ok, cut := enumerateCutInto(e.oppObs, e.e, pd, bot, e.cfg.kinds, e.cfg.oppLimit, e.cfg.autoPayment, e.cfg.enumBoard)
	if !ok {
		return nil, false
	}
	if cut {
		e.cfg.stats.Truncated++
	}
	if e.cfg.nameKeys {
		cands = nameKeys(e.oppObs, e.e, pd, cands, e.cfg.oppRootRefs)
	}
	e.cur, e.cands = pd, cands
	prior, fell := priors(e.cfg.priorNet(), e.e, pd, bot, kind, cands, e.cfg.priorView())
	if fell {
		e.cfg.stats.PriorFallbacks++
	}
	keys := make([]Key, len(cands))
	for i, c := range cands {
		keys[i] = c.key
	}
	return &Point{Keys: keys, Prior: prior, cut: cut, fell: fell, opp: !e.cfg.swapFrame}, true
}
