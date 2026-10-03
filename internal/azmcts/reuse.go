package azmcts

// Tree reuse (Options.ReuseTree): upstream MageZero keeps one tree per
// player for the whole game. After a move the chosen child becomes the
// root (ComputerPlayerMCTS.priority: root = best), and at the next decision
// ComputerPlayerMCTS2.getNextAction searches that subtree breadth first for
// the node whose state equals the real one (MCTSNode.getMatchingState: the
// encoder's state vector and GameState.getValue, both seen from the
// searching player), detaches it (emancipate) and keeps searching it, its
// visits and values intact, until the root holds searchBudget visits
// (applyMCTS); with no match it starts a fresh root. So the subtree
// survives the opponent's moves and anything else that happened in the
// tree, and the budget counts the visits the reused subtree already holds.
//
// Here, the seat's carrier (Reuse) keeps the tree of its last search that
// chose from its tree, and the root child it played. The next Search
// fingerprints the real engine -- its event chain head, event count, RNG
// draw count and pending decision sequence number, the identity package
// replay verifies -- and looks breadth first below that child for the node
// expanded from a world with the same fingerprint. Equal fingerprints mean
// the same event history from the same root state, so that node's world IS
// the real position: hidden zones and chance included, which is stricter
// than upstream's perspective-redacted match (it never matches a world
// whose draw, shuffle or hidden hand came out differently). Only the real
// world source qualifies (RealWorldSource: the clairvoyant clone, whose
// future chance is the real game's); Search refuses the rest.
//
// The match carries over by candidate, not by key: the new root's
// candidates are built as always (the full root, its macros, a fresh
// observer), and each one whose intent equals one of the stored node's
// candidates -- the same submit on the same engine -- inherits that child's
// visits, values, availability and subtree; a single-step macro counts as
// its one intent, a longer macro never matches. Root candidates the node
// lacks (the full root's macros and land plays, which in-walk points never
// offer) start fresh, and the node's children no root candidate names are
// dropped with their visits, as upstream's prune drops a child's. A full
// match keeps the node's own value and visits; a partial one re-totals
// them from the node's own evaluation and the kept children (edge.wUp). The
// root children's availability is set to the root's visits less its own
// evaluation (in a fixed world, the parent visits upstream's PUCT reads).
//
// Every simulation of the new search then walks a world rebuilt at the
// stored node's walk state: its observer lineage (keys below the root are
// numbered as the stored subtree's were), the NameKeys boundaries of the
// search that built it, the environment bots' streams, the step count and
// the engine clock. So a walk into a carried subtree reaches exactly the
// points the stored one did, and the node cache's fixed-world check holds.

import (
	"math/rand/v2"
	"reflect"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// Reuse is one seat's tree carrier across its decisions in one game
// (Options.ReuseTree; Root.Reuse). The zero value is empty. It is not safe
// for concurrent use: a seat's searches run one at a time.
type Reuse struct {
	// sub is the node below the root child the seat played: the only part
	// of the last tree a later position can be in.
	sub *node
	// rootRefs and oppRootRefs are the NameKeys boundaries of the search
	// that built top (walkConfig.rootRefs/oppRootRefs): a carried subtree's
	// in-walk keys were named against them.
	rootRefs, oppRootRefs int
}

// NewReuse is an empty carrier: the seat's first search builds a fresh
// tree.
func NewReuse() *Reuse { return &Reuse{} }

// Reset drops the stored tree.
func (r *Reuse) Reset() { r.sub = nil }

// worldPrint is a world's identity: two engines cloned from one state that
// print equal have applied the same events and drawn the same randomness.
type worldPrint struct {
	events int
	head   string
	draws  uint64
	seq    uint64
	// combat is the split-declaration progress (Options.CombatSteps,
	// combatWalk.printTag): the creature steps of one decision share the
	// engine's print and differ only here. Empty outside one.
	combat string
}

func printOf(e *rules.Engine) worldPrint {
	p := worldPrint{events: len(e.L.Events), head: e.L.Head(), draws: e.RNGDraws()}
	if d := e.Pending(); d != nil {
		p.seq = d.Seq
	}
	return p
}

// reuseMark is what making a node a later search's root needs: its world's
// print, the point's candidates (intents on that world, so on the real
// engine when the prints agree), and the walk state a world rebuilt there
// resumes from.
type reuseMark struct {
	print  worldPrint
	cands  []cand
	obs    *searchprobe.Collector
	oppObs *searchprobe.Collector
	pcgs   []rand.PCG
	actor  *rand.PCG
	opp    *rand.PCG
	steps  int
	plies  int
	// combat is the walk's split-declaration progress at the point
	// (Options.CombatSteps), nil outside one: its plan's keys are the ones
	// the stored subtree's creature steps were built with.
	combat *combatWalk
}

// reuseMarker is an Env that can mark the point it stands at.
type reuseMarker interface {
	reuseMark() *reuseMark
}

// markNode records env's walk state on nd, a newly expanded node of the
// searching seat (opponent nodes are never a root).
func markNode(nd *node, env Env) {
	if nd.opp {
		return
	}
	if m, ok := env.(reuseMarker); ok {
		nd.mark = m.reuseMark()
	}
}

// reuseMark is engineEnv's mark at its current point. The observers and
// candidates are kept, not copied: the env's walk ends at the point it
// marks (it has just expanded it), its observers are its own (the
// RealWorldSource promise), and a candidate list is never written after it
// is built.
func (e *engineEnv) reuseMark() *reuseMark {
	if e.e == nil || e.cur == nil || e.capped {
		return nil
	}
	pr := printOf(e.e)
	pr.combat = e.combat.printTag()
	m := &reuseMark{
		print: pr, cands: e.cands, obs: e.obs, oppObs: e.oppObs,
		pcgs: make([]rand.PCG, len(e.pcgs)), steps: e.steps, plies: e.plies,
		combat: e.combat.clone(),
	}
	for i, p := range e.pcgs {
		m.pcgs[i] = *p
	}
	if e.actorPCG != nil {
		a := *e.actorPCG
		m.actor = &a
	}
	if e.oppPCG != nil {
		o := *e.oppPCG
		m.opp = &o
	}
	return m
}

// resumeFrom positions a new world's walk state at m (walkConfig.resume):
// m's observer lineage instead of the world's fresh clone, its bot streams,
// step count and clock.
func (e *engineEnv) resumeFrom(m *reuseMark) {
	e.obs = m.obs.Clone()
	e.rngs, e.pcgs = make([]*rand.Rand, len(m.pcgs)), make([]*rand.PCG, len(m.pcgs))
	for i := range m.pcgs {
		p := m.pcgs[i]
		e.pcgs[i] = &p
		e.rngs[i] = rand.New(e.pcgs[i])
	}
	e.steps, e.plies = m.steps, m.plies
	if e.actorBot != nil && m.actor != nil {
		*e.actorPCG = *m.actor
	}
	if e.oppBot != nil && m.opp != nil {
		*e.oppPCG = *m.opp
	}
}

// find is the node below start (start included), breadth first in child
// order, whose world printed p.
func find(start *node, p worldPrint) *node {
	queue := []*node{start}
	for len(queue) > 0 {
		nd := queue[0]
		queue = queue[1:]
		if nd.mark != nil && nd.mark.print == p {
			return nd
		}
		for _, k := range nd.kids {
			if k.next != nil {
				queue = append(queue, k.next)
			}
		}
	}
	return nil
}

// sameSubmit reports whether a and b are the same submit.
func sameSubmit(a, b decision.Intent) bool {
	if a.Seq != b.Seq || a.Player != b.Player || !sameInts(a.Choices, b.Choices) || !sameInts(a.Rest, b.Rest) {
		return false
	}
	if (a.Payment == nil) != (b.Payment == nil) || (a.Announce == nil) != (b.Announce == nil) {
		return false
	}
	if a.Payment != nil && (a.Payment.ActionID != b.Payment.ActionID || !reflect.DeepEqual(a.Payment.Plan, b.Payment.Plan)) {
		return false
	}
	return a.Announce == nil || reflect.DeepEqual(*a.Announce, *b.Announce)
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// adopt is the reuse step of one Search: the root node to search from and
// the visits it already holds. real is the root engine (its log head is
// folded here, before any world clones it), cands and rootPt the new
// root's candidates and point. On a hit it points cfg at the stored node's
// walk state; on a miss it counts why, drops the stored tree and returns a
// fresh node.
func (r *Reuse) adopt(real *rules.Engine, cands []cand, rootPt *Point, cfg *walkConfig, st *Stats) (*node, int) {
	p := printOf(real)
	p.combat = cfg.rootCombat.printTag()
	fresh := newNode(rootPt)
	if r.sub == nil {
		st.ReuseMissNoTree++
		r.Reset()
		return fresh, 0
	}
	var nd *node
	if p.head != "" { // an unhashed log proves nothing
		nd = find(r.sub, p)
	}
	if nd == nil {
		st.ReuseMissState++
		r.Reset()
		return fresh, 0
	}
	m := nd.mark
	used := make([]bool, len(m.cands))
	kids := make([]*edge, len(cands))
	matched := 0
	for i, c := range cands {
		kids[i] = &edge{key: rootPt.Keys[i], prior: rootPt.Prior[i]}
		if c.macro != nil && len(c.macro.Steps) != 1 {
			continue
		}
		for j, sc := range m.cands {
			if used[j] || !sameSubmit(c.in, sc.in) {
				continue
			}
			old := nd.child(sc.key)
			if old == nil {
				continue
			}
			used[j] = true
			k := kids[i]
			k.n, k.w, k.wUp, k.next, k.end, k.endClk = old.n, old.w, old.wUp, old.next, old.end, old.endClk
			matched++
			break
		}
	}
	if matched == 0 {
		st.ReuseMissCandidates++
		r.Reset()
		return fresh, 0
	}
	top := &node{kids: kids, opp: rootPt.opp, v0: nd.v0}
	carried := 0
	for _, k := range kids {
		carried += k.n
	}
	partial := matched != len(cands) || matched != len(nd.kids)
	if partial {
		top.n, top.w = 1+carried, nd.v0
		for _, k := range kids {
			top.w += k.wUp
		}
		st.ReusePartial++
	} else {
		top.n, top.w = nd.n, nd.w
	}
	for _, k := range kids {
		k.avail = top.n - 1
	}
	rebaseSteps(top, m.steps)
	m.steps = 0
	cfg.resume = m
	if m.combat != nil {
		// The carried root is a creature step: the walks go on with the
		// stored walk's plan, whose keys (named by the stored observer
		// lineage) are the carried subtree's. Equal prints mean equal
		// answers so far.
		cfg.rootCombat = m.combat.clone()
	}
	cfg.rootRefs = r.rootRefs
	if cfg.oppNodes {
		cfg.oppObs, cfg.oppRootRefs = m.oppObs, r.oppRootRefs
	}
	st.ReuseHits++
	st.ReuseCarried += carried
	r.Reset()
	return top, carried
}

// rebaseSteps counts the env step cap (Options.MaxSteps, a per-simulation
// livelock guard) from the new root, not from the root of the search that
// built the carried tree, or a long chain of reused searches would start
// every walk with the cap spent: every carried mark's and stored state's
// step count drops by base, the steps the walk had taken to the new root.
// A walk the old count capped may now go on, so a carried capped end (the
// node cache's leaf record) is dropped and that edge is walked again; a
// game-over end stays.
func rebaseSteps(top *node, base int) {
	queue := []*node{top}
	for len(queue) > 0 {
		nd := queue[0]
		queue = queue[1:]
		if nd != top {
			if nd.mark != nil {
				nd.mark.steps -= base
			}
			if s, ok := nd.snap.(*envState); ok {
				s.steps -= base
			}
		}
		for _, k := range nd.kids {
			if k.end != nil && k.end.Capped {
				k.end = nil
			}
			if k.next != nil {
				queue = append(queue, k.next)
			}
		}
	}
}

// keep stores, for the seat's next search, the subtree below root child
// played of top, the tree of a search that chose it; the rest of the tree
// is let go. The node cache's states stored inside that subtree are kept
// (at most Options.NodeCache of them, each a few hundred KB, stay alive
// between the seat's decisions), so a walk into the carried subtree resumes
// where the last search stood instead of re-walking from the root.
func (r *Reuse) keep(top *node, played int, cfg *walkConfig) {
	r.sub = nil
	if played >= 0 && played < len(top.kids) {
		r.sub = top.kids[played].next
	}
	r.rootRefs, r.oppRootRefs = cfg.rootRefs, cfg.oppRootRefs
}
