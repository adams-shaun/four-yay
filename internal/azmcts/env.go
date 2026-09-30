package azmcts

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// walkConfig is what every simulation of one Search shares.
type walkConfig struct {
	net *policynet.Model
	// heuristicLeaf evaluates leaves with the frozen heuristic even when net
	// supplies the prior (Options.HeuristicLeaf).
	heuristicLeaf bool
	kinds         Kinds
	limit         int
	maxSteps      int
	envSeed       uint64
	actor         state.PlayerID
	autoPayment   bool
	// uniformPrior keeps the uniform prior at in-walk points even with a
	// network (Options.UniformPrior).
	uniformPrior bool
	// rootPerWorld re-derives the root candidates in every world
	// (Options.RootPerWorld).
	rootPerWorld bool
	// nameKeys names post-root objects in in-walk keys (Options.NameKeys);
	// rootRefs is the root observer's reference count after the root.
	nameKeys  bool
	rootRefs  int
	root      *Point
	rootCands []cand
	rootDec   *decision.Decision
	stats     *Stats
}

// priorNet is the network in-walk priors read: nil under uniformPrior.
func (c *walkConfig) priorNet() *policynet.Model {
	if c.uniformPrior {
		return nil
	}
	return c.net
}

// worldEnvs adapts a WorldSource to the tree's EnvSource.
type worldEnvs struct {
	src WorldSource
	cfg *walkConfig
}

func (w *worldEnvs) Env(sim int) (Env, error) {
	world, err := w.src.World(sim)
	if err != nil {
		return nil, err
	}
	env, err := newEngineEnv(world, w.cfg)
	if err != nil {
		return nil, err // never a typed-nil *engineEnv inside a non-nil Env
	}
	return env, nil
}

// engineEnv walks one world. The searching seat's searched decisions are
// the tree's points; botpolicy.Decide answers every other decision of both
// seats (spec §1's env step), from bot streams seeded identically for every
// simulation of the decision, so a clairvoyant clone is deterministic along
// a path.
type engineEnv struct {
	e      *rules.Engine
	obs    *searchprobe.Collector
	hyp    bool
	cfg    *walkConfig
	rngs   []*rand.Rand
	board  botpolicy.Board
	cur    *decision.Decision
	cands  []cand
	steps  int
	capped bool
	// plies counts every submit on this world since the root, the
	// searched intents included (PathEnv).
	plies int
	// root is this world's root point: cfg.root, or under rootPerWorld the
	// root keys this world offers.
	root *Point
	// actorBot answers the searching seat's unsearched decisions under
	// autoPayment: the auto-pay bot (seat.Bot.EnableAutoPayMana), so a
	// searched priority's bot answer is a planned cast or pass, never a
	// manual mana tap outside the vocabulary. Seeded identically for every
	// simulation, like rngs.
	actorBot *seat.Bot
}

func newEngineEnv(w World, cfg *walkConfig) (*engineEnv, error) {
	if w.Engine == nil || w.Observer == nil {
		return nil, fmt.Errorf("%w: the world has no engine or observer", ErrBadWorld)
	}
	pd, rd := w.Engine.Pending(), cfg.rootDec
	if w.Engine.G.Over || pd == nil || (pd.Seq != rd.Seq && !cfg.rootPerWorld) || pd.Player != rd.Player || pd.Kind != rd.Kind {
		return nil, fmt.Errorf("%w (root seq %d)", ErrBadWorld, rd.Seq)
	}
	n := len(w.Engine.G.Players)
	env := &engineEnv{
		e: w.Engine, obs: w.Observer, hyp: w.Hypothetical, cfg: cfg,
		rngs: searchprobe.BotRandoms(cfg.envSeed, n), board: botpolicy.NewBoard(n),
		cur: pd, cands: cfg.rootCands, root: cfg.root,
	}
	if cfg.autoPayment {
		env.actorBot = seat.NewBot(splitmix(cfg.envSeed ^ 0x6163746f722d6270 ^ uint64(cfg.actor))).EnableAutoPayMana()
	}
	if cfg.rootPerWorld {
		if err := env.matchRoot(); err != nil {
			return nil, err
		}
	}
	return env, nil
}

// matchRoot maps every root key onto this world's decision (IntentForKey):
// the world offers the keys it can play, with the root prior renormalised
// over them; a key it cannot play is unavailable in this simulation.
func (e *engineEnv) matchRoot() (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: matching the root: %v", ErrPanic, p)
		}
	}()
	m, err := newKeyMatcher(e.obs, e.e, e.cur)
	if err != nil {
		return fmt.Errorf("%w: observing the root: %v", ErrBadWorld, err)
	}
	root := e.cfg.root
	pt := &Point{}
	var cands []cand
	sum := 0.0
	for i, k := range root.Keys {
		in, err := m.intent(k)
		if err != nil {
			continue
		}
		cands = append(cands, cand{key: k, in: in})
		pt.Keys = append(pt.Keys, k)
		pt.Prior = append(pt.Prior, root.Prior[i])
		sum += root.Prior[i]
	}
	if len(cands) == 0 {
		return fmt.Errorf("%w: the world offers no root candidate", ErrBadWorld)
	}
	for i := range pt.Prior {
		if sum > 0 {
			pt.Prior[i] /= sum
		} else {
			pt.Prior[i] = 1 / float64(len(pt.Prior))
		}
	}
	e.cands, e.root = cands, pt
	return nil
}

func (e *engineEnv) Root() *Point { return e.root }

// Plies is PathEnv's engine clock: submits since the root.
func (e *engineEnv) Plies() int { return e.plies }

// Turn is PathEnv's turn number.
func (e *engineEnv) Turn() int { return int(e.e.G.Turn) }

// Play recovers a panic anywhere on its path -- the bot's answers
// (botpolicy.BoardFromGameInto, botpolicy.Decide), enumerate and priors at the
// next point (view.Project inside the prior), not only the engine's own
// Submit -- into ErrPanic, so one bad world discards one simulation instead
// of failing the game.
func (e *engineEnv) Play(k Key) (pt *Point, err error) {
	defer func() {
		if p := recover(); p != nil {
			pt, err = nil, fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	if e.cur == nil {
		return nil, fmt.Errorf("%w: play after the walk ended", ErrSubmit)
	}
	i := -1
	for j, c := range e.cands {
		if c.key == k {
			i = j
			break
		}
	}
	if i < 0 {
		return nil, fmt.Errorf("%w: candidate %s is not offered here", ErrSubmit, k)
	}
	if err := e.submit(e.cur, e.cands[i].in); err != nil {
		return nil, err
	}
	e.plies++
	return e.advance()
}

// advance answers decisions with the bot until the searching seat's next
// searched decision (a point), game over, or the step cap (nil). The cap
// bounds the bot's submits only: a searched decision reached with the cap
// exactly spent is still a point, so it expands rather than being evaluated
// as capped.
//
// Counters written here (EnvSteps, PriorFallbacks) are written as the walk
// goes, so a simulation discarded later still contributes them.
func (e *engineEnv) advance() (*Point, error) {
	for {
		g := e.e.G
		if g.Over {
			e.cur, e.cands = nil, nil
			return nil, nil
		}
		pd := e.e.Pending()
		if pd == nil {
			return nil, fmt.Errorf("%w: no pending decision and the game is not over", ErrSubmit)
		}
		b := botpolicy.BoardFromGameInto(g, e.e, pd.Player, &e.board)
		var in decision.Intent
		if pd.Player == e.cfg.actor && e.actorBot != nil {
			if pd.Kind == decision.KPriority {
				e.e.EnsurePaymentActions()
			}
			in, _ = e.actorBot.DecideBoard(context.Background(), b, *pd)
		} else {
			in = botpolicy.Decide(b, pd, e.rngs[pd.Player])
		}
		if pd.Player == e.cfg.actor {
			if cands, kind, _, ok, cut := enumerateCut(e.obs, e.e, pd, in, e.cfg.kinds, e.cfg.limit, e.cfg.autoPayment); ok {
				if cut {
					e.cfg.stats.Truncated++
				}
				if e.cfg.nameKeys {
					cands = nameKeys(e.e, pd, cands, e.cfg.rootRefs)
				}
				e.cur, e.cands = pd, cands
				prior, fell := priors(e.cfg.priorNet(), e.e, pd, in, kind, cands)
				if fell {
					e.cfg.stats.PriorFallbacks++
				}
				keys := make([]Key, len(cands))
				for i, c := range cands {
					keys[i] = c.key
				}
				return &Point{Keys: keys, Prior: prior}, nil
			}
		}
		if e.steps >= e.cfg.maxSteps {
			e.capped = true
			e.cur, e.cands = nil, nil
			return nil, nil
		}
		if err := e.submit(pd, in); err != nil {
			return nil, err
		}
		e.steps++
		e.plies++
		e.cfg.stats.EnvSteps++
	}
}

// submit plays in on the world, recovering any engine panic (the livelock
// watcher panics with *rules.LivelockError) into ErrPanic. A hypothetical
// world reports chance failures and ordinary rejections through one error,
// so an intent d itself rejects is classified first as ErrSubmit and any
// other SubmitHypothetical error is classified ErrChance.
func (e *engineEnv) submit(d *decision.Decision, in decision.Intent) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%w: %v", ErrPanic, p)
		}
	}()
	if !e.hyp {
		if serr := e.e.Submit(in); serr != nil {
			return fmt.Errorf("%w: %v", ErrSubmit, serr)
		}
		return nil
	}
	if verr := d.Validate(in); verr != nil {
		return fmt.Errorf("%w: %v", ErrSubmit, verr)
	}
	if herr := e.e.SubmitHypothetical(in); herr != nil {
		return fmt.Errorf("%w: %v", ErrChance, herr)
	}
	return nil
}

// Leaf is 1/0/0.5 at game over, else the leaf evaluator on the searching
// seat's redacted view. A panic in the evaluator (view.Project, the network)
// is recovered into Leaf.Err wrapping ErrPanic.
func (e *engineEnv) Leaf() (l Leaf) {
	defer func() {
		if p := recover(); p != nil {
			l = Leaf{Err: fmt.Errorf("%w: leaf: %v", ErrPanic, p)}
		}
	}()
	g := e.e.G
	if g.Over {
		v := 0.0
		switch {
		case g.Draw:
			v = 0.5
		case g.Winner == e.cfg.actor:
			v = 1
		}
		return Leaf{V: v, Terminal: true}
	}
	leafNet := e.cfg.net
	if e.cfg.heuristicLeaf {
		leafNet = nil
	}
	return Leaf{V: leafValue(leafNet, e.e, e.cfg.actor), Capped: e.capped}
}

// leafValue is spec §1's leaf: the value head on the actor's REDACTED view
// (policynet.Model.Value), or -- generation 0, no network -- the frozen
// heuristic searchprobe.LeafValue. Clamped into [0,1]; NaN reads 0.5.
func leafValue(net *policynet.Model, e *rules.Engine, actor state.PlayerID) float64 {
	v := view.Project(e.G, e, actor, e.Pending())
	if net == nil {
		return searchprobe.LeafValue(v, actor)
	}
	x := float64(net.Value(policynet.EncodeStateWith(net.Features, v, actor, nil)))
	switch {
	case math.IsNaN(x):
		return 0.5
	case x < 0:
		return 0
	case x > 1:
		return 1
	}
	return x
}
