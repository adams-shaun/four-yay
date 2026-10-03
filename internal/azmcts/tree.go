// Package azmcts is the AlphaZero-style tree search of
// docs/superpowers/specs/2026-09-27-alphazero-mcts-design.md: a PUCT tree
// over ONE seat's searched decisions (single perspective: every value is
// that seat's win probability), whose leaf is a value -- the policynet value
// head, or the frozen heuristic searchprobe.LeafValue for generation 0 --
// never a rollout. Under Options.OpponentNodes the tree also branches on the
// opponent's searched decisions; every stored value is still the searching
// seat's, and only selection at an opponent node reads it from the
// opponent's side (selectEdge).
//
// The package is pure search. It reads no clock (internal/archtest), ranges
// no map whose order could reach a choice, and draws randomness only from
// math/rand/v2 PCG streams its caller seeds, so the same seed, position and
// checkpoint give a byte-identical Result.
//
// Layers, bottom up:
//   - options.go, tree.go: knobs, counters, and the PUCT arithmetic over the
//     Env seam (RunTree), testable against a fake environment;
//   - candidates.go: the searched kinds, candidate enumeration over the
//     searchprobe enumerators, semantic keys, priors;
//   - world.go, env.go: where a simulation's world comes from (the
//     Clairvoyant source) and how it is walked (engineEnv);
//   - search.go: Search, the entry point;
//   - seat.go: the az seat, a searchseat.SearchSeat fed by internal/bench.
package azmcts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
)

// Key is one candidate's semantic identity at a node: the canonical encoding
// of its []searchprobe.Action (actionsKey). Equal keys name the same action
// in every world, which is what lets one tree be shared across worlds.
type Key string

// Point is one searched decision of the searching seat as the tree sees it.
type Point struct {
	// Keys are the candidates offered here; Keys[0] is the bot's answer.
	Keys []Key
	// Prior parallels Keys: non-negative, summing to 1.
	Prior []float64
	// cut and fell record what producing this point counted (an Env's
	// Stats.Truncated and PriorFallbacks), so the node cache, which reaches
	// a stored point without producing it again, counts them exactly as a
	// re-walk would have.
	cut, fell bool
	// opp marks the opponent's decision (Options.OpponentNodes): the node
	// expanded from it selects for the opponent. A world that offers a
	// node's decision to the other seat than the tree holds is an error.
	opp bool
}

// Leaf is the value of the position a walk stopped at, for the searching
// seat.
type Leaf struct {
	V        float64
	Terminal bool // the game is over (V is 1, 0 or 0.5)
	Capped   bool // the env step cap stopped the walk (V is the leaf evaluator's)
	// Err, when set, means the position could not be evaluated (the engine
	// env wraps a recovered panic in ErrPanic): the simulation is discarded
	// and counted by Err's class, and V is meaningless.
	Err error
}

// Env is one simulation's private world -- the fake-env seam spec §4 asks
// for ("clone, pending, submit, over, winner"): EnvSource.Env is the clone,
// Root and Play's returned Point are the pending searched decision, Play is
// the submit plus the environment's answers up to the next searched
// decision, and Leaf carries over/winner.
type Env interface {
	// Root is the root decision as this world offers it. A candidate the
	// tree knows but this world does not offer is unavailable here (spec §2,
	// the ISMCTS availability rule).
	Root() *Point
	// Play submits candidate k at the current point and advances to the
	// searching seat's next searched decision. A nil Point means the walk
	// ended (game over, or the step cap); an error discards the simulation.
	Play(k Key) (*Point, error)
	// Leaf evaluates the current position.
	Leaf() Leaf
}

// PathEnv is an optional Env extension: where the walk stands on the
// engine's own clock, for the backup discount and the depth diagnostics.
// An Env that does not implement it counts one ply per searched edge and
// never crosses a turn.
type PathEnv interface {
	// Plies is the number of engine submits made in this world since the
	// root: the searched intents and every environment answer.
	Plies() int
	// Turn is the game's turn number now.
	Turn() int
}

// EnvSource hands each simulation its own world.
type EnvSource interface {
	Env(sim int) (Env, error)
}

// NodeStateSource is an EnvSource over ONE fixed world: every Env it hands
// out is the same position with the same future chance, so a walk's state
// is a function of the keys it played and a simulation can resume from a
// state saved at a tree node instead of re-walking from the root (the node
// cache, Options.NodeCache). RunTree then asks Env for a world only until
// the root's state is saved, and never again.
type NodeStateSource interface {
	EnvSource
	// Save stores env's current position -- at a point Env.Root or Play
	// returned. With final false env keeps walking, on its own copy, so the
	// stored state never changes; final true means env's walk is over and
	// its state may be kept as it stands (env is not used again).
	Save(env Env, final bool) (any, error)
	// Resume is a fresh Env positioned at a saved state, as if it had
	// walked there from the root.
	Resume(snap any) (Env, error)
}

// TreeResult is the root's statistics after RunTree.
type TreeResult struct {
	Visits    []int     // per root key (parallel to root.Keys)
	Q         []float64 // mean backed-up value per root key; 0 when unvisited
	Avail     []int     // simulations in which each root key was available
	RootValue float64   // the root's mean value, its own evaluation included; 0.5 before any
}

type node struct {
	n    int
	w    float64
	kids []*edge
	// pt and snap are the node cache's (simulateCached): the point this
	// node was expanded from, and the saved env state at it (nil when not
	// stored). Both stay nil without the cache.
	pt   *Point
	snap any
	// clk is the engine clock at the node's point (PathEnv), which the
	// discount and the leaf-depth counters read; the node cache's.
	clk pathClock
	// opp is the point's owner (Point.opp): an opponent node.
	opp bool
	// v0 is the node's own evaluation, the leaf value it was expanded with
	// (the root's: its first simulation's root evaluation); mark is what
	// tree reuse needs to make this node a later search's root
	// (Options.ReuseTree, reuse.go), nil with the switch off and at every
	// opponent node.
	v0   float64
	mark *reuseMark
}

type edge struct {
	key   Key
	prior float64
	n     int
	w     float64
	avail int
	next  *node
	// end is the node cache's record of an edge whose walk ended (game
	// over or the step cap): in a fixed world every later play of it ends
	// the same way, so its leaf is reused instead of re-walked.
	end *Leaf
	// endClk is the engine clock where that walk ended.
	endClk pathClock
	// wUp sums the backed-up values this edge's simulations gave its
	// PARENT node (w is what they gave the child): equal to w without a
	// discount. Only tree reuse reads it, to re-total a carried root whose
	// children were dropped; it is kept only with Options.ReuseTree.
	wUp float64
}

func newNode(pt *Point) *node {
	nd := &node{kids: make([]*edge, len(pt.Keys)), opp: pt.opp}
	for i, k := range pt.Keys {
		nd.kids[i] = &edge{key: k, prior: pt.Prior[i]}
	}
	return nd
}

func (nd *node) q() float64 {
	if nd.n == 0 {
		return 0.5
	}
	return nd.w / float64(nd.n)
}

func (nd *node) child(k Key) *edge {
	for _, e := range nd.kids {
		if e.key == k {
			return e
		}
	}
	return nil
}

func hasKey(keys []Key, k Key) bool {
	for _, x := range keys {
		if x == k {
			return true
		}
	}
	return false
}

// puctScore is Q + c * P * sqrt(N_avail) / (1 + N) (spec §2).
func puctScore(q, prior float64, avail, n int, c float64) float64 {
	return q + c*prior*math.Sqrt(float64(avail))/float64(1+n)
}

// selectEdge is PUCT over the children this world offers. N_avail counts the
// simulations the child was available in, THIS one included (so the first
// selection at a node is guided by the prior rather than a 0 * U tie). An
// unvisited child's Q is the parent's Q minus FPU, or the constant
// opts.UnvisitedQ under opts.AbsoluteUnvisitedQ. Strict > keeps the lower
// index on a tie: candidate 0, the bot's answer, wins ties.
//
// At an opponent node (nd.opp) the mover is the opponent, so every Q is read
// in the mover's frame: the parent's is 1 - q, a visited child's 1 - w/n,
// first-play urgency subtracts FPU from the mover's parent Q, and the
// constant UnvisitedQ is taken as the mover's own. The stored values stay
// the searching seat's.
func selectEdge(nd *node, pt *Point, opts Options) *edge {
	parentQ := nd.q()
	if nd.opp {
		parentQ = 1 - parentQ
	}
	var best *edge
	bestScore := math.Inf(-1)
	for _, kid := range nd.kids {
		if !hasKey(pt.Keys, kid.key) {
			continue
		}
		q := parentQ - opts.FPU
		if opts.AbsoluteUnvisitedQ {
			q = opts.UnvisitedQ
		}
		if kid.n > 0 {
			q = kid.w / float64(kid.n)
			if nd.opp {
				q = 1 - q
			}
		}
		if s := puctScore(q, kid.prior, kid.avail+1, kid.n, opts.CPUCT); best == nil || s > bestScore {
			best, bestScore = kid, s
		}
	}
	return best
}

// RunTree runs opts.Sims simulations from root over src and returns the
// root's statistics. A ctx that is done -- checked between simulations --
// stops the loop where it is and increments st.DeadlineHits; Search then
// plays the bot's answer (Search's doc). A live context runs every
// simulation, unchanged.
//
// Each simulation takes a fresh Env, walks the tree by
// PUCT, expands exactly one new node (or stops where the walk ended),
// evaluates that position once and backs the value up the path: no rollouts
// (spec §2). A simulation whose Env or Play fails is discarded and counted in
// st by its error class; it changes no visit, value or availability count.
// The first Env the source produces also evaluates the root itself
// (AlphaZero's expansion value), the parent Q first-play urgency reads there.
//
// Children are kept in root order and ties go to the lower index, so
// root.Keys[0] -- the bot's answer -- wins every tie. No map is ranged.
func RunTree(ctx context.Context, root *Point, src EnvSource, opts Options, st *Stats) (TreeResult, error) {
	top, err := runTree(ctx, root, src, opts, st)
	if err != nil {
		return TreeResult{}, err
	}
	return treeResult(root, top), nil
}

// treeResult is the root's statistics of a tree runTree built from root.
func treeResult(root *Point, top *node) TreeResult {
	res := TreeResult{
		Visits: make([]int, len(root.Keys)), Q: make([]float64, len(root.Keys)),
		Avail: make([]int, len(root.Keys)), RootValue: top.q(),
	}
	for i := range root.Keys {
		k := top.kids[i]
		res.Visits[i], res.Avail[i] = k.n, k.avail
		if k.n > 0 {
			res.Q[i] = k.w / float64(k.n)
		}
	}
	return res
}

// runTree is RunTree's loop, returning the whole tree (the opponent-node
// tests read below the root).
func runTree(ctx context.Context, root *Point, src EnvSource, opts Options, st *Stats) (*node, error) {
	if root == nil || len(root.Keys) == 0 || len(root.Prior) != len(root.Keys) {
		return nil, fmt.Errorf("azmcts: a root point needs keys and a parallel prior")
	}
	return runTreeFrom(ctx, newNode(root), root, src, opts, opts.Sims, st), nil
}

// runTreeFrom runs sims simulations from top, a fresh node built from root
// or a carried one (tree reuse: its children are root's keys in order, its
// statistics kept), and returns it.
func runTreeFrom(ctx context.Context, top *node, root *Point, src EnvSource, opts Options, sims int, st *Stats) *node {
	var cache *nodeCache
	if ns, ok := src.(NodeStateSource); ok && opts.NodeCache > 0 {
		top.pt = root
		cache = &nodeCache{src: ns, max: opts.NodeCache}
		if opts.ReuseTree {
			cache.stored = carriedStates(top)
		}
	}
	for i := 0; i < sims; i++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				// The armed wall-clock bail-out (Search's doc): stop
				// between simulations; Search plays the bot's answer.
				st.DeadlineHits++
				break
			}
		}
		st.Simulations++
		if cache != nil {
			if err := cache.simulate(top, i, opts, st); err != nil {
				classify(st, err)
				continue
			}
			st.Completed++
			continue
		}
		env, err := src.Env(i)
		if err != nil {
			classify(st, err)
			continue
		}
		if err := simulate(top, env, opts, st); err != nil {
			classify(st, err)
			continue
		}
		st.Completed++
	}
	return top
}

// simulate is one simulation. Availability marks, visits and values are
// committed only when it succeeds.
//
// One exception: the root's own evaluation is committed by the first
// simulation that produces it, before that simulation's walk is known to
// succeed. A walk that then fails keeps the root's value (it read the root
// world, which was positioned correctly), so RootValue and first-play urgency
// can rest on a simulation that Completed does not count.
func simulate(top *node, env Env, opts Options, st *Stats) error {
	if top.n == 0 {
		l := env.Leaf()
		if l.Err != nil {
			return l.Err
		}
		top.n, top.w, top.v0 = 1, l.V, l.V
	}
	var (
		nodes   []*node
		path    []*edge
		marks   []*edge
		unavail int
		// clock[i] is the engine clock at nodes[i]'s decision; the leaf's
		// is appended when the walk ends.
		clock []pathClock
	)
	pe, _ := env.(PathEnv)
	now := func() pathClock {
		if pe == nil {
			return pathClock{plies: len(path)}
		}
		return pathClock{plies: pe.Plies(), turn: pe.Turn()}
	}
	nd, pt := top, env.Root()
	for {
		if pt == nil || len(pt.Keys) == 0 || len(pt.Prior) != len(pt.Keys) {
			return fmt.Errorf("%w: a world offered a malformed point", ErrSubmit)
		}
		if pt.opp != nd.opp {
			return errOwner
		}
		for _, kid := range nd.kids {
			if hasKey(pt.Keys, kid.key) {
				marks = append(marks, kid)
			} else {
				unavail++
			}
		}
		for j, k := range pt.Keys {
			if nd.child(k) == nil {
				kid := &edge{key: k, prior: pt.Prior[j]}
				nd.kids = append(nd.kids, kid)
				marks = append(marks, kid)
			}
		}
		sel := selectEdge(nd, pt, opts)
		if nd.opp {
			st.OppPoints++
		}
		clock = append(clock, now())
		nodes, path = append(nodes, nd), append(path, sel)
		replay, steps0 := sel.next != nil || sel.n > 0, st.EnvSteps
		next, err := env.Play(sel.key)
		st.Plays++
		if replay {
			st.ReplayPlays++
			st.ReplaySteps += st.EnvSteps - steps0
		}
		if err != nil {
			return err
		}
		if next == nil {
			l := env.Leaf()
			if l.Err != nil {
				return l.Err
			}
			if l.Terminal {
				st.Terminal++
			}
			if l.Capped {
				st.StepCapped++
			}
			commit(nodes, path, marks, append(clock, now()), l.V, opts, st)
			st.Unavailable += unavail
			return nil
		}
		if sel.next == nil {
			l := env.Leaf()
			if l.Err != nil {
				return l.Err
			}
			sel.next = newNode(next)
			sel.next.n, sel.next.w, sel.next.v0 = 1, l.V, l.V
			st.Expanded++
			if next.opp {
				st.OppExpanded++
			}
			if opts.ReuseTree {
				markNode(sel.next, env)
			}
			commit(nodes, path, marks, append(clock, now()), l.V, opts, st)
			st.Unavailable += unavail
			return nil
		}
		nd, pt = sel.next, next
	}
}

// errOwner is a world offering a tree node's decision to the other seat
// than the node was expanded for (Point.opp): the world broke the tree's
// shape, and the simulation is discarded.
var errOwner = fmt.Errorf("%w: a world offered a node's decision to the other seat", ErrSubmit)

// pathClock is the engine clock at one point of a walk (PathEnv).
type pathClock struct{ plies, turn int }

// commit backs v up a completed walk. clock parallels nodes plus one: its
// last entry is the leaf's. Node i stands clock[i] on the engine clock and
// its edge path[i] leads to the position at clock[i+1], so under a discount
// node i's value is v's deviation from 0.5 scaled by gamma^Delta for the
// Delta between clock[i] and the leaf, and edge i's by the Delta between
// clock[i+1] and the leaf (the child's own position; the newly expanded
// leaf keeps v undiscounted, as upstream's backprop adds the leaf's value
// before its first multiplication).
func commit(nodes []*node, path, marks []*edge, clock []pathClock, v float64, opts Options, st *Stats) {
	leaf := clock[len(clock)-1]
	discounted := opts.Discount > 0 && opts.Discount < 1
	at := func(i int) float64 {
		if !discounted {
			return v
		}
		var delta int
		switch opts.DiscountUnit {
		case DiscountAction:
			delta = len(path) - i
		case DiscountTurn:
			delta = leaf.turn - clock[i].turn
		default:
			delta = leaf.plies - clock[i].plies
		}
		return discountValue(v, opts.Discount, delta)
	}
	for i, n := range nodes {
		n.n++
		n.w += at(i)
	}
	for i, e := range path {
		e.n++
		e.w += at(i + 1)
		if opts.ReuseTree {
			e.wUp += at(i)
		}
	}
	for _, e := range marks {
		e.avail++
	}
	st.LeafPlies += leaf.plies - clock[0].plies
	st.LeafEdges += len(path)
	st.LeafTurns += leaf.turn - clock[0].turn
}

// discountValue is 0.5 + (v - 0.5) * gamma^delta: the discount shrinks a
// [0,1] value toward 0.5 ("unknown") as upstream's shrinks a [-1,1] value
// toward 0. A non-positive delta leaves v unchanged.
func discountValue(v, gamma float64, delta int) float64 {
	if delta <= 0 {
		return v
	}
	return 0.5 + (v-0.5)*math.Pow(gamma, float64(delta))
}

func classify(st *Stats, err error) {
	switch {
	case errors.Is(err, ErrChance):
		st.ChanceFailures++
	case errors.Is(err, ErrPanic):
		st.Panics++
	case errors.Is(err, ErrBadWorld):
		st.BadWorlds++
	case errors.Is(err, ErrNoWorld):
		st.NoWorld++
	default:
		st.SubmitErrors++
	}
}

// choose is the move: the most-visited root child (ties to the lower index),
// or -- generation only -- a draw proportional to visits (tau = 1).
func choose(visits []int, sample bool, rng *rand.Rand) int {
	best := 0
	for i := 1; i < len(visits); i++ {
		if visits[i] > visits[best] {
			best = i
		}
	}
	if !sample || rng == nil {
		return best
	}
	total := 0
	for _, v := range visits {
		total += v
	}
	if total == 0 {
		return best
	}
	r := rng.IntN(total)
	for i, v := range visits {
		if r < v {
			return i
		}
		r -= v
	}
	return best
}

func uniform(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1 / float64(n)
	}
	return out
}

// noisyPrior is (1-eps)*prior + eps*Dirichlet(alpha) (spec §2).
func noisyPrior(prior []float64, rng *rand.Rand, alpha, eps float64) []float64 {
	eta := dirichlet(rng, len(prior), alpha)
	out := make([]float64, len(prior))
	for i, p := range prior {
		out[i] = (1-eps)*p + eps*eta[i]
	}
	return out
}

// dirichlet draws a symmetric Dirichlet(alpha) vector of length n:
// independent Gamma(alpha, 1) draws, normalised.
func dirichlet(rng *rand.Rand, n int, alpha float64) []float64 {
	out := make([]float64, n)
	sum := 0.0
	for i := range out {
		out[i] = gammaDraw(rng, alpha)
		sum += out[i]
	}
	if sum <= 0 {
		return uniform(n)
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

// gammaDraw is Marsaglia and Tsang's Gamma(alpha, 1) sampler; alpha < 1 uses
// the boost Gamma(alpha) = Gamma(alpha+1) * U^(1/alpha).
func gammaDraw(rng *rand.Rand, alpha float64) float64 {
	if alpha < 1 {
		u := rng.Float64()
		for u == 0 {
			u = rng.Float64()
		}
		return gammaDraw(rng, alpha+1) * math.Pow(u, 1/alpha)
	}
	d := alpha - 1.0/3
	c := 1 / math.Sqrt(9*d)
	for {
		x := rng.NormFloat64()
		v := 1 + c*x
		if v <= 0 {
			continue
		}
		v = v * v * v
		u := rng.Float64()
		if u < 1-0.0331*x*x*x*x || math.Log(u) < 0.5*x*x+d*(1-v+math.Log(v)) {
			return d * v
		}
	}
}
