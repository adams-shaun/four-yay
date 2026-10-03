package azmcts

import (
	"context"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// The tree-reuse tests (Options.ReuseTree). A "game" here is the real
// engine driven between the seat's searches by an engineEnv over the REAL
// engine built from the finished search's walk configuration: it plays the
// root candidate the search chose (and, with opponent nodes, an opponent
// reply the tree explored), answering every other decision exactly as the
// search's walks did, so the next position is one the stored tree holds.

// reuseKnobs are the knob sets the reuse tests run: the spec's defaults
// (manual payment, six candidates, first-play urgency) and the search
// benchmark's / mzplay's (bench=true: auto-pay, the full root with land
// macros, name keys, the absolute unvisited Q, the action discount).
func reuseKnobs(bench bool, sims int, seed uint64) Options {
	o := DefaultOptions()
	o.Sims, o.Seed = sims, seed
	if bench {
		o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = 0.5, true, 0.5
		o.Limit, o.AutoPayment, o.UniformPrior, o.NameKeys = BenchCandidateLimit, true, true, true
		o.Discount, o.DiscountUnit = 0.99, DiscountAction
	}
	o.ReuseTree = true
	return o
}

// reuseStep is one search of a reuse sequence and its walk configuration.
type reuseStep struct {
	res Result
	cfg *walkConfig
	obs *searchprobe.Collector
	top *node // the whole tree the search built
}

// reuseSearchAt searches e's pending decision with carrier r (nil with the
// switch off). The root's bot answer is the default bot's (rng seeded by
// seq); under bench knobs the root is the full root with land macros.
func reuseSearchAt(t *testing.T, e *rules.Engine, r *Reuse, opts Options, src func(*rules.Engine, *searchprobe.Collector) WorldSource) reuseStep {
	t.Helper()
	d := e.Pending()
	bot := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, rand.New(rand.NewPCG(d.Seq, 7)))
	obs := searchprobe.NewCollector(d.Player)
	root := Root{Engine: e, Decision: d, Bot: bot, Observer: obs, Reuse: r}
	if opts.AutoPayment {
		root.Macros, root.BotKey = landMacros(d, bot)
		root.NoBot = true
	}
	if src == nil {
		src = func(e *rules.Engine, obs *searchprobe.Collector) WorldSource {
			s, _ := newTestClairvoyant(e, obs)
			return s
		}
	}
	var cfg *walkConfig
	var top *node
	searchCfgHook = func(c *walkConfig) { cfg = c }
	searchTreeHook = func(n *node) { top = n }
	defer func() { searchCfgHook, searchTreeHook = nil, nil }()
	res, err := Search(context.Background(), root, src(e, obs), nil, opts)
	if err != nil {
		t.Fatalf("seq %d: %v", d.Seq, err)
	}
	return reuseStep{res: res, cfg: cfg, obs: obs, top: top}
}

// drive plays step's chosen root candidate on the real engine e exactly as
// the search's walks played it, then -- when the walk stops at an opponent
// node -- the first opponent reply whose subtree reaches a node of the
// searching seat, and returns true when e then stands at a decision of the
// searching seat the stored tree holds a node for.
func drive(t *testing.T, e *rules.Engine, step reuseStep, r *Reuse) bool {
	t.Helper()
	res := step.res
	if res.Stats.Searched != 1 || step.cfg == nil {
		return false
	}
	env, err := newEngineEnv(World{Engine: e, Observer: step.obs.Clone()}, step.cfg)
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	pt, err := env.Play(res.Keys[res.Choice])
	if err != nil {
		t.Fatalf("driver: play %q: %v", res.Keys[res.Choice], err)
	}
	if pt == nil || r == nil || r.sub == nil {
		return false
	}
	nd := r.sub
	for nd != nil && pt != nil && pt.opp {
		var next *node
		var key Key
		for _, k := range nd.kids {
			if k.next != nil && reaches(k.next) {
				next, key = k.next, k.key
				break
			}
		}
		if next == nil {
			return false
		}
		if pt, err = env.Play(key); err != nil {
			t.Fatalf("driver: opponent reply %q: %v", key, err)
		}
		nd = next
	}
	return pt != nil && !pt.opp && nd != nil && nd.mark != nil
}

// reaches reports whether nd's subtree holds a marked node of the
// searching seat along first-expanded opponent replies.
func reaches(nd *node) bool {
	if !nd.opp {
		return nd.mark != nil
	}
	for _, k := range nd.kids {
		if k.next != nil && reaches(k.next) {
			return true
		}
	}
	return false
}

// reuseRoots are positions to start reuse sequences from.
func reuseRoots(t *testing.T, n int) []measureRoot {
	return measureRoots(t, n)
}

// cloneRoot is r's engine cloned, so several sequences can start there.
func cloneRoot(r measureRoot) *rules.Engine { return r.e.Clone() }

// --- Hit after the seat's own move ------------------------------------------

// After the seat's move, when the game reaches the position the tree's walk
// reached, the next search carries that node: one hit, its visits counted
// against the budget (Sims - carried new simulations), the carried visits in
// the root's visits, the root value and Q's carried, every root child
// available as often as the root was visited.
func TestReuseHitAfterOwnMove(t *testing.T) {
	roots := reuseRoots(t, 6)
	hits, partial, full, equal := 0, 0, 0, 0
	for i, rt := range roots {
		for _, bench := range []bool{false, true} {
			e := cloneRoot(rt)
			r := NewReuse()
			opts := reuseKnobs(bench, 150, uint64(i)*31+5)
			s1 := reuseSearchAt(t, e, r, opts, nil)
			if s1.res.Stats.ReuseMissNoTree != 1 || s1.res.Stats.ReuseHits != 0 {
				t.Fatalf("%s: first search stats %+v, want one no-tree miss", rt.name, s1.res.Stats)
			}
			if !drive(t, e, s1, r) {
				continue
			}
			s2 := reuseSearchAt(t, e, r, opts, nil)
			st := s2.res.Stats
			if st.ReuseHits != 1 {
				t.Fatalf("%s bench %v: second search missed: %+v", rt.name, bench, st)
			}
			sum := 0
			for _, v := range s2.res.Visits {
				sum += v
			}
			if st.ReuseCarried <= 0 || st.Simulations != max(0, opts.Sims-st.ReuseCarried) || sum != st.Completed+st.ReuseCarried {
				t.Fatalf("%s bench %v: carried %d simulations %d completed %d visits %d", rt.name, bench, st.ReuseCarried, st.Simulations, st.Completed, sum)
			}
			for k, a := range s2.res.Avail {
				if want := sum; a != want {
					t.Fatalf("%s bench %v: avail[%d] %d, want the root's %d visits", rt.name, bench, k, a, want)
				}
			}
			hits++
			if st.ReusePartial == 1 {
				partial++
				continue
			}
			full++
			// Upstream's budget makes a fully matched carried node, in a
			// fixed world, the tree a fresh search from here grows -- PUCT
			// at a node reads only that node's statistics -- as long as the
			// environment answers as it did for the stored walks.
			off := opts
			off.ReuseTree = false
			fresh := reuseSearchAt(t, e.Clone(), nil, off, nil)
			if sameTree(s2.res, fresh.res) {
				equal++
			}
		}
	}
	if hits < 4 {
		t.Fatalf("only %d hits", hits)
	}
	if full > 0 && equal == 0 {
		t.Fatalf("no full hit grew a fresh search's tree (%d full hits)", full)
	}
	t.Logf("%d hits (%d full, %d partial); %d full hits are exactly the fresh search's tree", hits, full, partial, equal)
}

// sameTree reports whether two searches' root statistics and choices are
// identical, bit for bit (their cost and reuse counters aside).
func sameTree(a, b Result) bool {
	a.Stats, b.Stats = Stats{}, Stats{}
	return sameResult(a, b) == ""
}

// A reused root that already holds the whole budget runs no simulation and
// plays the carried statistics' move.
func TestReuseCarriedBudgetRunsNothing(t *testing.T) {
	for i, rt := range reuseRoots(t, 6) {
		e := cloneRoot(rt)
		r := NewReuse()
		opts := reuseKnobs(false, 400, uint64(i)+77)
		s1 := reuseSearchAt(t, e, r, opts, nil)
		if !drive(t, e, s1, r) {
			continue
		}
		opts.Sims = 1
		s2 := reuseSearchAt(t, e, r, opts, nil)
		st := s2.res.Stats
		if st.ReuseHits != 1 || st.ReuseCarried < 1 {
			continue
		}
		if st.Simulations != 0 || st.AllFailed != 0 || s2.res.Intent.Seq != e.Pending().Seq {
			t.Fatalf("%s: stats %+v intent %+v", rt.name, st, s2.res.Intent)
		}
		best := 0
		for k, v := range s2.res.Visits {
			if v > s2.res.Visits[best] {
				best = k
			}
		}
		if s2.res.Choice != best {
			t.Fatalf("%s: choice %d, visits %v", rt.name, s2.res.Choice, s2.res.Visits)
		}
		return
	}
	t.Fatal("no root carried a hit")
}

// --- Hit after an opponent move ----------------------------------------------

// With opponent nodes the tree holds the opponent's replies; after the seat's
// move and an opponent reply the tree explored, the next search finds the
// node below that reply.
func TestReuseHitAfterOpponentMove(t *testing.T) {
	hits := 0
	for i, rt := range reuseRoots(t, 8) {
		for _, bench := range []bool{false, true} {
			e := cloneRoot(rt)
			r := NewReuse()
			opts := reuseKnobs(bench, 200, uint64(i)*13+3)
			opts.OpponentNodes = true
			s1 := reuseSearchAt(t, e, r, opts, nil)
			if s1.res.Stats.Searched != 1 {
				continue
			}
			nd := r.sub
			if nd == nil || !nd.opp {
				continue // the move ended the walk, or no opponent node followed it
			}
			if !drive(t, e, s1, r) {
				continue
			}
			s2 := reuseSearchAt(t, e, r, opts, nil)
			if s2.res.Stats.ReuseHits != 1 || s2.res.Stats.ReuseCarried < 1 {
				t.Fatalf("%s bench %v: after an opponent reply the tree explored: %+v", rt.name, bench, s2.res.Stats)
			}
			hits++
		}
	}
	if hits < 2 {
		t.Fatalf("only %d hits through an opponent node", hits)
	}
	t.Logf("%d hits through an opponent node", hits)
}

// --- Misses --------------------------------------------------------------------

// Chance the walk did not see (the real engine drew randomness the world
// did not), and a move the tree never played, are both a state miss: the
// next search builds a fresh tree, exactly as a search without reuse.
func TestReuseMissesWhenTheWorldDiffers(t *testing.T) {
	checked := 0
	for i, rt := range reuseRoots(t, 6) {
		e := cloneRoot(rt)
		r := NewReuse()
		opts := reuseKnobs(false, 120, uint64(i)+9)
		s1 := reuseSearchAt(t, e, r, opts, nil)
		if s1.res.Stats.Searched != 1 {
			continue
		}
		// Hidden information came out differently: the real engine's
		// generator moved where no world's did.
		e.Rand(2)
		if !drive(t, e, s1, r) {
			continue
		}
		s2 := reuseSearchAt(t, e, r, opts, nil)
		if s2.res.Stats.ReuseMissState != 1 || s2.res.Stats.ReuseHits != 0 || s2.res.Stats.ReuseCarried != 0 {
			t.Fatalf("%s: after unseen chance: %+v", rt.name, s2.res.Stats)
		}
		// The miss searched a fresh tree: its result is a no-reuse search's.
		off := opts
		off.ReuseTree = false
		e2 := e.Clone()
		plain := reuseSearchAt(t, e2, nil, off, nil)
		if d := sameResult(withoutReuse(s2.res), plain.res); d != "" {
			t.Fatalf("%s: a missed search differs from a fresh one: %s", rt.name, d)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("only %d misses checked", checked)
	}
	// A position the tree never reached: the game went on with a root
	// move the search never explored (two simulations, three or more
	// candidates).
	for i, rt := range reuseRoots(t, 12) {
		e := cloneRoot(rt)
		r := NewReuse()
		opts := reuseKnobs(false, 2, uint64(i)+1)
		s1 := reuseSearchAt(t, e, r, opts, nil)
		if s1.res.Stats.Searched != 1 || len(s1.res.Candidates) < 3 {
			continue
		}
		other := -1
		for k, kid := range s1.top.kids {
			if kid.next == nil && kid.end == nil && kid.n == 0 {
				other = k
				break
			}
		}
		if other < 0 {
			continue
		}
		actor := e.Pending().Player
		if err := e.Submit(s1.res.Candidates[other]); err != nil {
			continue
		}
		for steps := 0; steps < 400 && !e.G.Over && e.Pending() != nil; steps++ {
			d := e.Pending()
			if d.Player == actor {
				if _, _, ok := enumerate(searchprobe.NewCollector(actor), e, d, botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, rand.New(rand.NewPCG(d.Seq, 7))), AllKinds(), opts.Limit); ok {
					break
				}
			}
			if err := e.Submit(botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, rand.New(rand.NewPCG(d.Seq, 1)))); err != nil {
				t.Fatal(err)
			}
		}
		if e.G.Over || e.Pending() == nil || e.Pending().Player != actor {
			continue
		}
		s2 := reuseSearchAt(t, e, r, opts, nil)
		if s2.res.Stats.Searched != 1 {
			continue
		}
		if s2.res.Stats.ReuseMissState != 1 || s2.res.Stats.ReuseHits != 0 {
			t.Fatalf("%s: after an unexplored move: %+v", rt.name, s2.res.Stats)
		}
		return
	}
	t.Fatal("no unexplored move to play")
}

// withoutReuse is st with the reuse counters zeroed.
func withoutReuse(r Result) Result {
	r.Stats.ReuseHits, r.Stats.ReusePartial, r.Stats.ReuseCarried = 0, 0, 0
	r.Stats.ReuseMissNoTree, r.Stats.ReuseMissState, r.Stats.ReuseMissCandidates = 0, 0, 0
	return r
}

// --- Off and the first search ---------------------------------------------------

// The first search of a seat (nothing stored) is a search without reuse:
// the same Result but the one no-tree miss.
func TestReuseFirstSearchIsAPlainSearch(t *testing.T) {
	for i, rt := range reuseRoots(t, 4) {
		for _, bench := range []bool{false, true} {
			for _, opp := range []bool{false, true} {
				on := reuseKnobs(bench, 80, uint64(i)+3)
				on.OpponentNodes = opp
				off := on
				off.ReuseTree = false
				a := reuseSearchAt(t, cloneRoot(rt), NewReuse(), on, nil)
				b := reuseSearchAt(t, cloneRoot(rt), nil, off, nil)
				if a.res.Stats.ReuseMissNoTree != 1 {
					t.Fatalf("%s: first search %+v", rt.name, a.res.Stats)
				}
				if d := sameResult(withoutReuse(a.res), b.res); d != "" {
					t.Fatalf("%s bench %v opp %v: the first reuse search differs from a plain one: %s", rt.name, bench, opp, d)
				}
			}
		}
	}
}

// --- Determinism and the node cache --------------------------------------------

// A reuse sequence (search, drive, search, drive, search) is deterministic,
// and the node cache off and on (256, and a cap small enough to evict) give
// the same Results but for the walk's cost counters -- with and without
// opponent nodes, under both knob sets.
func TestReuseDeterministicAndCacheExact(t *testing.T) {
	roots := reuseRoots(t, 6)
	sims := 120
	if testing.Short() {
		roots, sims = roots[:3], 60
	}
	hits := 0
	for i, rt := range roots {
		for _, bench := range []bool{false, true} {
			for _, opp := range []bool{false, true} {
				run := func(cache int) []Result {
					e := cloneRoot(rt)
					r := NewReuse()
					opts := reuseKnobs(bench, sims, uint64(i)*7+1)
					opts.OpponentNodes = opp
					opts.NodeCache = cache
					var out []Result
					for k := 0; k < 3; k++ {
						s := reuseSearchAt(t, e, r, opts, nil)
						out = append(out, s.res)
						if k == 2 || !drive(t, e, s, r) {
							break
						}
					}
					return out
				}
				base := run(0)
				if again := run(0); !reflect.DeepEqual(base, again) {
					t.Fatalf("%s bench %v opp %v: two runs differ", rt.name, bench, opp)
				}
				for _, res := range base {
					hits += res.Stats.ReuseHits
				}
				for _, cache := range []int{3, DefaultNodeCache} {
					got := run(cache)
					if len(got) != len(base) {
						t.Fatalf("%s bench %v opp %v cache %d: %d searches vs %d", rt.name, bench, opp, cache, len(got), len(base))
					}
					for k := range got {
						if d := sameResult(base[k], got[k]); d != "" {
							t.Fatalf("%s bench %v opp %v cache %d search %d: %s", rt.name, bench, opp, cache, k, d)
						}
					}
				}
			}
		}
	}
	if hits == 0 {
		t.Fatal("no sequence hit")
	}
	t.Logf("%d hits across the sequences", hits)
}

// --- Refusals ----------------------------------------------------------------------

func TestReuseRefusals(t *testing.T) {
	for _, o := range []Options{
		func() Options { o := reuseKnobs(false, 8, 1); o.RootPerWorld = true; return o }(),
	} {
		if o.Validate(nil) == nil {
			t.Errorf("options %+v accepted", o)
		}
	}
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, "", 0, 2000)
	obs := searchprobe.NewCollector(d.Player)
	search := func(src WorldSource, r *Reuse, o Options) error {
		_, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs, Reuse: r}, src, nil, o)
		return err
	}
	for name, src := range map[string]WorldSource{ // each case on its own: order reaches nothing
		"fixed-chance": &FixedChance{Base: e, Observer: obs, Seed: 3},
		"re-seeded":    hypSource{e: e, obs: obs},
	} {
		if err := search(src, NewReuse(), reuseKnobs(false, 8, 1)); err == nil || !strings.Contains(err.Error(), "real world") {
			t.Errorf("%s: err %v", name, err)
		}
	}
	clair, _ := newTestClairvoyant(e, obs)
	if err := search(clair, nil, reuseKnobs(false, 8, 1)); err == nil {
		t.Error("the switch without a carrier accepted")
	}
	off := reuseKnobs(false, 8, 1)
	off.ReuseTree = false
	if err := search(clair, NewReuse(), off); err == nil {
		t.Error("a carrier without the switch accepted")
	}
	if err := search(clair, NewReuse(), reuseKnobs(false, 8, 1)); err != nil {
		t.Errorf("the clairvoyant source refused: %v", err)
	}
	sc := DefaultSeatConfig()
	sc.World, sc.Search.ReuseTree = WorldRedeal, true
	if _, err := NewSeat(1, nil, sc); err == nil {
		t.Error("a redeal seat with tree reuse accepted")
	}
	sc.World, sc.Source = WorldClairvoyant, testSeatSource
	if _, err := NewSeat(1, nil, sc); err != nil {
		t.Errorf("a clairvoyant seat with tree reuse refused: %v", err)
	}
}

// The carrier keeps only the subtree below the root child the seat played,
// with at most Options.NodeCache stored engine states in it, and the next
// search's node cache starts from those states.
func TestReuseKeepsOnlyThePlayedSubtree(t *testing.T) {
	resumed := 0
	for i, rt := range reuseRoots(t, 6) {
		e := cloneRoot(rt)
		r := NewReuse()
		opts := reuseKnobs(true, 200, uint64(i)+5)
		opts.NodeCache = 40
		s := reuseSearchAt(t, e, r, opts, nil)
		if s.res.Stats.Searched != 1 {
			continue
		}
		if r.sub != s.top.kids[s.res.Choice].next {
			t.Fatalf("%s: the carrier does not hold the played child's subtree", rt.name)
		}
		if r.sub == nil {
			continue
		}
		if n := len(carriedStates(&node{kids: []*edge{{next: r.sub}}})); n > opts.NodeCache {
			t.Fatalf("%s: %d stored states carried, cap %d", rt.name, n, opts.NodeCache)
		}
		if !drive(t, e, s, r) {
			continue
		}
		s2 := reuseSearchAt(t, e, r, opts, nil)
		if s2.res.Stats.ReuseHits == 1 {
			resumed += s2.res.Stats.NodeResumes
		}
	}
	if resumed == 0 {
		t.Fatal("no carried search resumed from a stored state")
	}
}

// The step cap counts from the new root: carried marks and stored states
// drop the steps taken to it, a capped end is walked again, a game-over end
// stays.
func TestReuseRebasesTheStepCap(t *testing.T) {
	leaf := &node{mark: &reuseMark{steps: 40}, snap: &envState{steps: 41}}
	mid := &node{mark: &reuseMark{steps: 25}, kids: []*edge{
		{key: "x", next: leaf},
		{key: "y", end: &Leaf{V: 0.4, Capped: true}},
		{key: "z", end: &Leaf{V: 1, Terminal: true}},
	}}
	top := &node{kids: []*edge{{key: "a", next: mid}}}
	rebaseSteps(top, 20)
	if mid.mark.steps != 5 || leaf.mark.steps != 20 || leaf.snap.(*envState).steps != 21 {
		t.Fatalf("steps %d %d %d", mid.mark.steps, leaf.mark.steps, leaf.snap.(*envState).steps)
	}
	if mid.kids[1].end != nil || mid.kids[2].end == nil {
		t.Fatalf("ends: capped %v terminal %v", mid.kids[1].end, mid.kids[2].end)
	}
}
