package azmcts

// The node state cache (Options.NodeCache): over a FIXED world -- one
// position and one future chance for every simulation (FixedWorldSource) --
// a walk's state is a function of the keys it played, so a simulation
// resumes from the state saved at the deepest stored node of its path
// instead of re-walking the tree's edges from the root (upstream
// BenchSearch caches a game state at every node the same way). Measured
// before the cache (TestNodeCacheMeasure, 12 roots, clairvoyant): 73% of a
// simulation's env steps re-walked already-expanded edges at 100
// simulations, 89% at 1000, 91% at 3000.
//
// It is exact: with the cache on, the tree sees the same points, leaves and
// selections as without it, so the Result is byte-identical except the
// walk's cost counters (Stats.EnvSteps, PriorFallbacks, Plays, ReplayPlays,
// ReplaySteps and the Node* counters) -- TestNodeCacheIsExact pins it over
// many roots. It is never on for a source whose worlds differ between
// simulations (the redeal source, a re-seeded chance): there a node's state
// is not a function of its path.
//
// What is stored, and when:
//
//   - the root's state, by the first simulation that reaches it (one copy);
//   - a newly expanded node's state, while there is room: that walk is over,
//     so its env is kept as it stands, with no copy;
//   - an unstored node a simulation walks through (it was expanded while the
//     cache was full, or its state was evicted): while there is room, or by
//     evicting the least-visited stored node (never the root) when that node
//     has strictly fewer visits than this one (one copy);
//   - an edge whose walk ended (game over, the step cap): its leaf, so the
//     next simulation down it touches no engine at all.
//
// Options.NodeCache caps the stored node states (the root's included); an
// engine state is roughly one engine clone (TestNodeCacheMeasure reports
// the peak RSS). Eviction drops the state for the garbage collector and
// never releases its arrays for reuse: a stored state's descendants may
// share its event-log prefix (rules.Engine.Release's contract).

import (
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// DefaultNodeCache is DefaultOptions' node-state cap.
const DefaultNodeCache = 256

// nodeCache is one RunTree's stored node states.
type nodeCache struct {
	src NodeStateSource
	max int
	// stored holds every node whose snap is set; stored[0] is the root once
	// its state is saved. No map: eviction scans it in insertion order.
	stored []*node
}

func validPoint(pt *Point) bool {
	return pt != nil && len(pt.Keys) > 0 && len(pt.Prior) == len(pt.Keys)
}

// samePoint reports whether a replayed walk reached the point nd was
// expanded from: the fixed-world promise, checked on every replayed edge.
func samePoint(pt *Point, nd *node) bool {
	if pt == nil || nd.pt == nil || len(pt.Keys) != len(nd.pt.Keys) {
		return false
	}
	for i, k := range pt.Keys {
		if nd.pt.Keys[i] != k {
			return false
		}
	}
	return true
}

// save stores env's state at nd. final means env's walk is over: the state
// is taken as it stands, with no copy.
func (c *nodeCache) save(nd *node, env Env, final bool, st *Stats) {
	snap, err := c.src.Save(env, final)
	if err != nil || snap == nil {
		return
	}
	nd.snap = snap
	c.stored = append(c.stored, nd)
	st.NodeSaves++
}

// offer stores env's state at nd, an unstored tree node the walk has just
// reached, if there is room or a less-visited node to evict.
func (c *nodeCache) offer(nd *node, env Env, final bool, st *Stats) {
	if len(c.stored) < c.max {
		c.save(nd, env, final, st)
		return
	}
	if final {
		// A new leaf (one visit) never displaces a stored node: every
		// stored node has at least one visit and ties keep the incumbent.
		return
	}
	victim := -1
	for i := 1; i < len(c.stored); i++ { // stored[0] is the root: never evicted
		if victim < 0 || c.stored[i].n < c.stored[victim].n {
			victim = i
		}
	}
	if victim < 0 || c.stored[victim].n >= nd.n {
		return
	}
	c.stored[victim].snap = nil
	c.stored = append(c.stored[:victim], c.stored[victim+1:]...)
	st.NodeEvicts++
	c.save(nd, env, final, st)
}

// simulate is one simulation over the cache: RunTree's simulate with the
// walk's prefix skipped. The tree walk -- availability, new children,
// selection, backup -- is simulate's, read from each node's stored point
// (in a fixed world the point a node was expanded from is the point every
// later walk reaches there). The env is positioned at the deepest stored
// node on the selected path and plays only the edges below it; a replayed
// edge that reaches a different point than the tree holds is an error (the
// source broke its fixed-world promise), never a silent mismatch.
func (c *nodeCache) simulate(top *node, sim int, opts Options, st *Stats) error {
	var env Env
	if top.snap == nil {
		e, err := c.src.Env(sim)
		if err != nil {
			return err
		}
		env = e
		if top.n == 0 {
			l := env.Leaf()
			if l.Err != nil {
				return l.Err
			}
			top.n, top.w = 1, l.V
		}
		top.pt = env.Root()
		if !validPoint(top.pt) {
			top.pt = nil
			return fmt.Errorf("%w: a world offered a malformed point", ErrSubmit)
		}
		c.save(top, env, false, st)
	}
	var (
		nodes   []*node
		path    []*edge
		marks   []*edge
		unavail int
		from    int // index in nodes of the node env is (or will be) positioned at
	)
	nd := top
	for {
		pt := nd.pt
		if !validPoint(pt) {
			return fmt.Errorf("%w: a world offered a malformed point", ErrSubmit)
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
		nodes, path = append(nodes, nd), append(path, sel)
		if sel.next == nil {
			break
		}
		nd = sel.next
		if env == nil && nd.snap != nil {
			from = len(nodes)
		}
	}
	last := path[len(path)-1]
	if last.end != nil {
		l := *last.end
		if l.Terminal {
			st.Terminal++
		}
		if l.Capped {
			st.StepCapped++
		}
		commit(nodes, path, marks, l.V)
		st.Unavailable += unavail
		return nil
	}
	if env == nil {
		e, err := c.src.Resume(nodes[from].snap)
		if err != nil {
			return err
		}
		env = e
		if from > 0 {
			st.NodeResumes++
		}
	}
	for j := from; j < len(path); j++ {
		sel := path[j]
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
		if j < len(path)-1 {
			if !samePoint(next, nodes[j+1]) {
				return fmt.Errorf("%w: a fixed world reached a different point than the tree holds", ErrSubmit)
			}
			if nodes[j+1].snap == nil {
				c.offer(nodes[j+1], env, false, st)
			}
			continue
		}
		l := env.Leaf()
		if l.Err != nil {
			return l.Err
		}
		if next == nil {
			if l.Terminal {
				st.Terminal++
			}
			if l.Capped {
				st.StepCapped++
			}
			end := l
			sel.end = &end
			commit(nodes, path, marks, l.V)
			st.Unavailable += unavail
			return nil
		}
		sel.next = newNode(next)
		sel.next.pt = next
		sel.next.n, sel.next.w = 1, l.V
		st.Expanded++
		commit(nodes, path, marks, l.V)
		st.Unavailable += unavail
		c.offer(sel.next, env, true, st)
		return nil
	}
	return errors.New("azmcts: unreachable: the cached walk ended without a leaf")
}

// fixedEnvs is worldEnvs over a FixedWorldSource: the NodeStateSource the
// node cache walks. It asks the world source for worlds until the root's
// state is saved; every later simulation is a copy of a saved state.
type fixedEnvs struct {
	*worldEnvs
	// prev is the last walker this source made (Resume, or a Save that
	// moved a walker onto its own copy): simulations run one at a time, so
	// it is spent when the next is resumed, and its arrays build that one.
	// A walker whose state was kept (a final Save) is never released.
	prev  *engineEnv
	spare rules.Spare
}

// envState is one saved engineEnv position. Its engine and observer are
// never written again: Resume walks copies.
type envState struct {
	e     *rules.Engine
	obs   *searchprobe.Collector
	hyp   bool
	pcgs  []rand.PCG
	cands []cand
	steps int
}

func (f *fixedEnvs) Save(env Env, final bool) (snap any, err error) {
	ee, ok := env.(*engineEnv)
	if !ok || ee.e == nil || ee.cur == nil || ee.capped {
		return nil, errors.New("azmcts: the node cache saves an engine env at a point")
	}
	s := &envState{e: ee.e, obs: ee.obs, hyp: ee.hyp, cands: ee.cands, steps: ee.steps, pcgs: make([]rand.PCG, len(ee.pcgs))}
	for i, p := range ee.pcgs {
		s.pcgs[i] = *p
	}
	if final {
		// The walk is over: the state is ee's own engine, kept as it stands.
		ee.e, ee.obs, ee.cur, ee.cands = nil, nil, nil, nil
		if f.prev == ee {
			f.prev = nil
		}
		return s, nil
	}
	defer func() {
		if p := recover(); p != nil {
			snap, err = nil, fmt.Errorf("%w: node cache save: %v", ErrPanic, p)
		}
	}()
	// The walk goes on: it moves onto its own copy, and the state keeps
	// the engine it stood on.
	c := s.e.CloneInto(&f.spare)
	ee.e, ee.obs = c, s.obs.Clone()
	ee.cur = c.Pending()
	f.prev = ee
	return s, nil
}

func (f *fixedEnvs) Resume(snap any) (env Env, err error) {
	s, ok := snap.(*envState)
	if !ok {
		return nil, errors.New("azmcts: not a node cache state")
	}
	if f.prev != nil {
		if f.prev.e != nil {
			f.spare = f.prev.e.Release()
		}
		f.prev.e = nil
		f.prev = nil
	}
	defer func() {
		if p := recover(); p != nil {
			env, err = nil, fmt.Errorf("%w: node cache resume: %v", ErrPanic, p)
		}
	}()
	e := s.e.CloneInto(&f.spare)
	rngs, pcgs := make([]*rand.Rand, len(s.pcgs)), make([]*rand.PCG, len(s.pcgs))
	for i := range s.pcgs {
		p := s.pcgs[i]
		pcgs[i] = &p
		rngs[i] = rand.New(pcgs[i])
	}
	ee := &engineEnv{
		e: e, obs: s.obs.Clone(), hyp: s.hyp, cfg: f.cfg,
		rngs: rngs, pcgs: pcgs, board: botpolicy.NewBoard(len(e.G.Players)),
		cur: e.Pending(), cands: s.cands, steps: s.steps,
	}
	f.prev = ee
	return ee, nil
}
