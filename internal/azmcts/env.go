package azmcts

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"sync"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
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
	// priorTopK is the PriorTopK in effect (Options.priorTopK): 0, or the
	// candidates rankedPrior keeps at every in-walk point of the searching
	// seat, which then enumerates past limit (enumLimit).
	priorTopK int
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
	// envBoard and enumBoard are the Search's board scratch
	// (botpolicy.BoardFromGameInto): simulations run one at a time, and
	// every board is read only until the decision it was built for is
	// answered, so one pair serves every simulation of the Search instead
	// of a fresh map set per simulation and per enumerated decision.
	// envBoard backs the bot's answers (engineEnv.advance), enumBoard the
	// candidate enumeration, so neither refill clobbers a board still read.
	envBoard, enumBoard *boardScratch
	// views is the Search's projection scratch (viewScratch): the leaf's
	// and the prior's reusable views, refilled per leaf and per point.
	// Nil (a hand-built config) projects into fresh views.
	views *viewScratch
	// oppNodes makes the other seats' searched decisions tree points
	// (Options.OpponentNodes). oppForks[p] hands seat p's walk a fresh
	// collector per world (nil for the searching seat): simulations run one
	// at a time, so one forked collector, rolled back, serves them all, as
	// the redeal source's forker does for the searching seat.
	oppNodes bool
	// skipPass is Options.SkipPass.
	skipPass int
	oppForks []*searchprobe.Forker
}

// priorNet is the network in-walk priors read: nil under uniformPrior.
func (c *walkConfig) priorNet() *policynet.Model {
	if c.uniformPrior {
		return nil
	}
	return c.net
}

// viewScratch is one Search's reusable projections. Simulations run one at
// a time and each view is read only until the value or prior it feeds is
// computed, so one leaf view and one prior view serve every simulation;
// Search draws a scratch from searchViews and returns it when it ends, so a
// worker's searches reuse the same buffers decision after decision.
type viewScratch struct {
	leaf, prior view.View
	leafChars   heuristicLeafChars
}

var searchViews = sync.Pool{New: func() any { return new(viewScratch) }}

// heuristicLeafChars is the frozen heuristic leaf's view.Chars: the real
// engine's, minus every derived fact searchprobe.LeafValue never reads.
// LeafValue reads the result (Over/Draw/Winner) and, per seat, Life,
// HandSize and each battlefield card's Types, Power and Toughness -- nothing
// else -- so the seat's own potential-action walk (a whole second legal-offer
// walk per leaf), the own-library list, the genesis manifest, availability,
// ability costs, keywords, card tokens and the library-top reveal are not
// computed for it. Embedding the interface also drops the optional layer-3
// Name and effective-cost capabilities (cards keep their printed names and
// costs, which the leaf does not read either). The leaf value is unchanged:
// TestHeuristicLeafCharsKeepsLeafValue pins it against the full projection.
type heuristicLeafChars struct{ view.Chars }

func (heuristicLeafChars) PotentialActions(state.PlayerID) []decision.PotentialAction { return nil }
func (heuristicLeafChars) OwnDeck(state.PlayerID) *deck.Manifest                      { return nil }
func (heuristicLeafChars) AvailableMana(state.PlayerID) state.Mana                    { return state.Mana{} }
func (heuristicLeafChars) AbilityCosts(state.PlayerID, state.ObjID) []string          { return nil }
func (heuristicLeafChars) Keywords(state.ObjID) []string                              { return nil }
func (heuristicLeafChars) MayLookAtLibraryTop(state.PlayerID) bool                    { return false }
func (heuristicLeafChars) SuppressOwnLibrary() bool                                   { return true }
func (heuristicLeafChars) SuppressCardTokens() bool                                   { return true }

// priorView is the config's reusable prior projection, nil without scratch.
func (c *walkConfig) priorView() *view.View {
	if c.views == nil {
		return nil
	}
	return &c.views.prior
}

// enumLimit is the searching seat's enumeration cap at an in-walk point:
// limit, or under priorTopK every candidate (BenchCandidateLimit), for
// rankedPrior to cut.
func (c *walkConfig) enumLimit() int {
	if c.priorTopK > 0 {
		return BenchCandidateLimit
	}
	return c.limit
}

// candKeys is cands' keys, in order.
func candKeys(cands []cand) []Key {
	keys := make([]Key, len(cands))
	for i, c := range cands {
		keys[i] = c.key
	}
	return keys
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
// the tree's points -- and under Options.OpponentNodes the opponent's too
// (oppPoint); botpolicy.Decide answers every other decision of both
// seats (spec §1's env step), from bot streams seeded identically for every
// simulation of the decision, so a clairvoyant clone is deterministic along
// a path.
type engineEnv struct {
	e      *rules.Engine
	obs    *searchprobe.Collector
	hyp    bool
	cfg    *walkConfig
	rngs   []*rand.Rand
	pcgs   []*rand.PCG // rngs' sources, which the node cache saves and restores
	board  *botpolicy.Board
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
	// actorPCG is actorBot's source, which the node cache saves and
	// restores with pcgs.
	actorPCG *rand.PCG
	// oppObs[p] is opponent p's collector in this world
	// (Options.OpponentNodes), forked on p's first decision the walk
	// enumerates and fed by every later one. It starts empty in every
	// world: the opponent's hand is re-dealt per world, so its references
	// name nothing across worlds, and opponent keys name objects by card
	// name instead (oppPoint). Nil until used.
	oppObs []*searchprobe.Collector
}

func newEngineEnv(w World, cfg *walkConfig) (*engineEnv, error) {
	if w.Engine == nil || w.Observer == nil {
		return nil, fmt.Errorf("%w: the world has no engine or observer", ErrBadWorld)
	}
	pd, rd := w.Engine.Pending(), cfg.rootDec
	if w.Engine.G.Over || pd == nil || (pd.Seq != rd.Seq && !cfg.rootPerWorld) || pd.Player != rd.Player || pd.Kind != rd.Kind {
		return nil, fmt.Errorf("%w (root seq %d)", ErrBadWorld, rd.Seq)
	}
	// A simulation's world and every decision it poses die together: the
	// env reads a posed decision only until it answers it, and the sources
	// Release a world only when the next simulation asks for its own. So
	// the world's priority decisions come from its recyclable decision arena
	// (rules.Engine.SetDecisionArena) instead of fresh allocations.
	w.Engine.SetDecisionArena(true)
	w.Engine.SetSkipPass(cfg.skipPass)
	n := len(w.Engine.G.Players)
	var board *botpolicy.Board
	if cfg.envBoard != nil {
		board = cfg.envBoard.board(n)
	} else {
		b := botpolicy.NewBoard(n)
		board = &b
		b.SkipOwnLibrary()
	}
	rngs, pcgs := botStreams(cfg.envSeed, n)
	env := &engineEnv{
		e: w.Engine, obs: w.Observer, hyp: w.Hypothetical, cfg: cfg,
		rngs: rngs, pcgs: pcgs, board: board,
		cur: pd, cands: cfg.rootCands, root: cfg.root,
	}
	if cfg.autoPayment {
		env.actorPCG = seat.BotSource(splitmix(cfg.envSeed ^ 0x6163746f722d6270 ^ uint64(cfg.actor)))
		env.actorBot = seat.NewBotOn(env.actorPCG).EnableAutoPayMana()
	}
	if cfg.rootPerWorld {
		if err := env.matchRoot(); err != nil {
			return nil, err
		}
	}
	return env, nil
}

// botStreams is searchprobe.BotRandoms with the PCG sources kept, so the
// node cache can save a walk's bot streams mid-game (math/rand/v2's Rand
// holds no state beyond its source). TestBotStreamsAreBotRandoms pins the
// two identical.
func botStreams(seed uint64, seats int) ([]*rand.Rand, []*rand.PCG) {
	rngs, pcgs := make([]*rand.Rand, seats), make([]*rand.PCG, seats)
	for i := range rngs {
		s := seed ^ uint64(i+1)
		pcgs[i] = rand.NewPCG(s, s^0x9e3779b97f4a7c15)
		rngs[i] = rand.New(pcgs[i])
	}
	return rngs, pcgs
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
		var c cand
		if IsMacroKey(k) {
			mc, ok := e.macroCand(k)
			if !ok {
				continue
			}
			c = mc
		} else {
			in, err := m.intent(k)
			if err != nil {
				continue
			}
			c = cand{key: k, in: in}
		}
		cands = append(cands, c)
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
		return nil, fmt.Errorf("%w: candidate %q is not offered here", ErrSubmit, k)
	}
	if m := e.cands[i].macro; m != nil {
		if err := e.playMacro(m); err != nil {
			return nil, err
		}
	} else if err := e.submit(e.cur, e.cands[i].in); err != nil {
		return nil, err
	}
	e.plies++
	return e.advance()
}

// advance answers decisions with the bot until the searching seat's next
// searched decision (a point) -- or, under Options.OpponentNodes, an
// opponent's (oppPoint) -- game over, or the step cap (nil). The cap
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
		var in decision.Intent
		// skippedPay marks a priority window whose payment actions were not
		// built ahead of the bot's answer (see below); they are built later
		// only where something reads them.
		skippedPay := false
		if pd.Player == e.cfg.actor && e.actorBot != nil {
			b := botpolicy.BoardFromGameInto(g, e.e, pd.Player, e.board)
			if pd.Kind == decision.KPriority {
				// seat.Bot.paymentIntent declines outside a main phase on the
				// bot's own turn (MyTurn && !IsMain) and never reads
				// PaymentActions there, so the (costly) build is deferred:
				// enumerate builds it for a searched point, and a capped stop
				// builds it below, so every reader sees what it saw before.
				if b.MyTurn && !b.IsMain {
					skippedPay = true
				} else {
					e.e.EnsurePaymentActions()
				}
			}
			in, _ = e.actorBot.DecideBoard(context.Background(), b, *pd)
		} else {
			// A decision whose bot answer reads no board (a priority window
			// offering nothing but pass, or only mana activations outside a main
			// phase) skips the board build: botpolicy.DecideBoardFree is Decide's
			// own answer there and, like it, draws no rng.
			var free bool
			in, free = botpolicy.DecideBoardFree(pd, g.Step.IsMain())
			if !free {
				b := botpolicy.BoardFromGameInto(g, e.e, pd.Player, e.board)
				in = botpolicy.Decide(b, pd, e.rngs[pd.Player])
			}
		}
		if pd.Player == e.cfg.actor {
			if cands, kind, _, ok, cut := enumerateCutInto(e.obs, e.e, pd, in, e.cfg.kinds, e.cfg.enumLimit(), e.cfg.autoPayment, e.cfg.enumBoard); ok {
				if cut {
					e.cfg.stats.Truncated++
				}
				if e.cfg.nameKeys {
					cands = nameKeys(e.obs, e.e, pd, cands, e.cfg.rootRefs, e.cfg.enumBoard)
				}
				// Under priorTopK the network keeps the point's best
				// candidates (after nameKeys, which may merge two).
				cands, prior, fell, before := rankedPrior(e.cfg.priorNet(), e.cfg.priorTopK, e.e, pd, in, kind, cands, e.cfg.priorView())
				e.cur, e.cands = pd, cands
				if fell {
					e.cfg.stats.PriorFallbacks++
				}
				if e.cfg.priorTopK > 0 {
					countTopK(e.cfg.stats, true, before)
					return &Point{Keys: candKeys(cands), Prior: prior, cut: cut, fell: fell, ranked: true, before: before}, nil
				}
				keys := make([]Key, len(cands))
				for i, c := range cands {
					keys[i] = c.key
				}
				return &Point{Keys: keys, Prior: prior, cut: cut, fell: fell}, nil
			}
		} else if e.cfg.oppNodes {
			if pt := e.oppPoint(pd, in); pt != nil {
				return pt, nil
			}
		}
		if e.steps >= e.cfg.maxSteps {
			if skippedPay {
				e.e.EnsurePaymentActions()
			}
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

// oppKeyPrefix marks an opponent's candidate key (Options.OpponentNodes).
// Two worlds can reach one tree node with different seats to act (the
// opponent has a block to choose in one, none in the other), so the node
// holds both seats' children; the prefix keeps them apart, and each seat's
// are unavailable while the other acts. No other key starts with it (a
// semantic action key's first byte is 0 or 1; payKeyPrefix and
// MacroKeyPrefix differ).
const oppKeyPrefix = "opp:"

// oppPoint is Options.OpponentNodes at opponent pd.Player's decision pd,
// the walk bot's answer in: the tree point there, or nil when pd is not one
// -- not a searched kind, fewer than two candidates (same-named candidates
// counted once), a bot answer outside the vocabulary, a translation failure
// -- and the bot's answer is played as before.
//
// The candidates are the searching seat's enumerators over the opponent's
// per-world collector (the bot's answer first, capped at the limit), in the
// manual-payment vocabulary: the walk bot that answers the opponent pays
// manually, so a cast is offered only once its mana floats and a tap is
// never a candidate. Every key names its objects by card name (nameKeys
// with nothing known at the root) behind oppKeyPrefix. The prior is the
// network's on the OPPONENT's redacted view (priors projects pd.Player's),
// uniform without one. Truncated and PriorFallbacks count here as at the
// searching seat's points.
func (e *engineEnv) oppPoint(pd *decision.Decision, in decision.Intent) *Point {
	obs := e.oppObserver(pd.Player)
	if obs == nil {
		return nil
	}
	cands, kind, _, ok, cut := enumerateCutInto(obs, e.e, pd, in, e.cfg.kinds, e.cfg.limit, false, e.cfg.enumBoard)
	if !ok {
		return nil
	}
	if cands = nameKeys(obs, e.e, pd, cands, 0, e.cfg.enumBoard); len(cands) < 2 {
		return nil
	}
	if cut {
		e.cfg.stats.Truncated++
	}
	keys := make([]Key, len(cands))
	for i := range cands {
		cands[i].key = Key(oppKeyPrefix + string(cands[i].key))
		keys[i] = cands[i].key
	}
	e.cur, e.cands = pd, cands
	prior, fell := priors(e.cfg.priorNet(), e.e, pd, in, kind, cands, e.cfg.priorView())
	if fell {
		e.cfg.stats.PriorFallbacks++
	}
	return &Point{Keys: keys, Prior: prior, cut: cut, fell: fell, Opp: true}
}

// oppObserver is seat p's collector in this world, forked on first use; nil
// for a seat with no fork (the searching seat, or an out-of-range seat).
func (e *engineEnv) oppObserver(p state.PlayerID) *searchprobe.Collector {
	if int(p) >= len(e.cfg.oppForks) || e.cfg.oppForks[p] == nil {
		return nil
	}
	if e.oppObs == nil {
		e.oppObs = make([]*searchprobe.Collector, len(e.cfg.oppForks))
	}
	if e.oppObs[p] == nil {
		e.oppObs[p] = e.cfg.oppForks[p].Fork()
	}
	return e.oppObs[p]
}

// cloneObservers is a deep copy of a walk's opponent collectors
// (engineEnv.oppObs). The node cache stores and resumes copies: a forked
// collector is rolled back by the next fork, and a stored state is never
// written again.
func cloneObservers(obs []*searchprobe.Collector) []*searchprobe.Collector {
	if obs == nil {
		return nil
	}
	out := make([]*searchprobe.Collector, len(obs))
	for i, c := range obs {
		if c != nil {
			out[i] = c.Clone()
		}
	}
	return out
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
	return Leaf{V: leafValue(leafNet, e.e, e.cfg.actor, e.cfg.views), Capped: e.capped}
}

// leafValue is spec §1's leaf: the value head on the actor's REDACTED view
// (policynet.Model.Value), or -- generation 0, no network -- the frozen
// heuristic searchprobe.LeafValue, computed straight from the game
// (heuristicLeafValue, bit-identical to the view-based value). Clamped into
// [0,1]; NaN reads 0.5. A network leaf projects into sc's reusable view when
// sc is non-nil (view.ProjectInto).
func leafValue(net *policynet.Model, e *rules.Engine, actor state.PlayerID, sc *viewScratch) float64 {
	if net == nil {
		return heuristicLeafValue(e, actor)
	}
	if sc == nil {
		sc = new(viewScratch)
	}
	v := &sc.leaf
	view.ProjectInto(v, e.G, e, actor, e.Pending())
	x := float64(net.Value(policynet.EncodeStateWith(net.Features, *v, actor, nil)))
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

// heuristicLeafValue is searchprobe.LeafValue(view.Project(e.G, e, actor,
// e.Pending()), actor) computed straight off the game, without building the
// view: the frozen heuristic reads only each player's life, hand size and
// battlefield (searchprobe.LeafScore and its material), so projecting every
// zone, hand, library, mana availability and potential action per leaf was
// allocation the value never read. Every fact is the one the projection
// would have carried -- the same objects (view's cardViews filter: faced,
// non-ephemeral, phased-in), the same face-down redaction for this viewer
// (Seat visibility, no also-visible seats), the printed face's types, the
// engine's derived power and toughness -- summed in the same order, so the
// float is bit-identical (TestHeuristicLeafMatchesTheView).
func heuristicLeafValue(e *rules.Engine, actor state.PlayerID) float64 {
	g := e.G
	if g.Over {
		switch {
		case g.Draw:
			return 0.5
		case int(g.Winner) < len(g.Players) && g.Winner == actor:
			return 1
		default:
			return 0
		}
	}
	value := 0.0
	for i := range g.Players {
		p := &g.Players[i]
		score := float64(p.Life) + 2*float64(len(g.Zone(state.ZHand, p.ID)))
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil || o.Face() == nil || o.Ephemeral() || o.PhasedOut {
				continue
			}
			score += leafMaterial(e, o, actor)
		}
		if p.ID == actor {
			value += score
		} else {
			value -= score
		}
	}
	return 1 / (1 + math.Exp(-value/20))
}

// leafMaterial is searchprobe's material over the object's CardView as the
// actor's projection would build it.
func leafMaterial(e *rules.Engine, o *state.Object, actor state.PlayerID) float64 {
	if o.FaceDown {
		looker := o.Controller
		if o.HasMayLook {
			looker = o.MayLookPlayer
		}
		if o.Zone == state.ZPlanarDeck || actor != looker {
			// The redacted face-down view carries no types: neither a land
			// nor a creature.
			return 10
		}
	}
	f := o.Face()
	// strings.Contains over the space-joined type list is a match inside
	// one type word (neither needle holds a space).
	land, creature := false, false
	for _, t := range f.Types {
		land = land || strings.Contains(t, "Land")
		creature = creature || strings.Contains(t, "Creature")
	}
	if land {
		return 3
	}
	score := 10.0
	if creature {
		score += 2 * float64(e.Power(o.ID)+e.Toughness(o.ID))
	}
	return score
}
