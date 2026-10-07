package azmcts

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
)

// At an opponent's point selection reads every Q as 1 - Q: of two equally
// visited, equally likely children it takes the one worse for the searching
// seat, where the seat's own point takes the better.
func TestSelectEdgeReadsTheOpponentsSide(t *testing.T) {
	nd := &node{n: 3, w: 1.5, kids: []*edge{
		{key: "x", prior: 0.5, n: 1, w: 0.9, avail: 1},
		{key: "y", prior: 0.5, n: 1, w: 0.1, avail: 1},
	}}
	keys := []Key{"x", "y"}
	if got := selectEdge(nd, &Point{Keys: keys}, DefaultOptions()).key; got != "x" {
		t.Fatalf("the seat's own point selected %q, want x", got)
	}
	if got := selectEdge(nd, &Point{Keys: keys, Opp: true}, DefaultOptions()).key; got != "y" {
		t.Fatalf("an opponent's point selected %q, want y", got)
	}
	// First-play urgency from the opponent's side: the parent is worth 0.8
	// to the seat, 0.2 to the opponent, so an unvisited reply reads 0.1
	// there and loses to a visited one the opponent values at 0.7; at the
	// seat's own point it reads 0.7 and wins against a visited 0.3.
	nd = &node{n: 2, w: 1.6, kids: []*edge{
		{key: "x", prior: 0.5, n: 1, w: 0.3, avail: 1},
		{key: "y", prior: 0.5, avail: 1},
	}}
	o := DefaultOptions()
	o.CPUCT = 0.01 // Q decides
	if got := selectEdge(nd, &Point{Keys: keys, Opp: true}, o).key; got != "x" {
		t.Fatalf("an opponent's point selected %q, want the visited x", got)
	}
	if got := selectEdge(nd, &Point{Keys: keys}, o).key; got != "y" {
		t.Fatalf("the seat's own point selected %q, want the unvisited y", got)
	}
}

// The same two-level game, its second level the seat's own point or the
// opponent's: the seat's own reply x wins (1), so a is worth taking; the
// opponent replies y (the seat loses), so b's sure 0.6 is better.
func TestOpponentPointMinimisesTheSeatsValue(t *testing.T) {
	for _, opp := range []bool{false, true} {
		g := &fakeGame{nodes: map[string]fakeNode{
			"":     {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
			"/a":   {keys: []Key{"x", "y"}, prior: []float64{0.5, 0.5}, value: 0.5, opp: opp},
			"/a/x": {value: 1, terminal: true},
			"/a/y": {value: 0, terminal: true},
			"/b":   {value: 0.6, terminal: true},
		}}
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(200), &st)
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if opp {
			want = 1
		}
		if c := choose(tr.Visits, false, nil); c != want {
			t.Fatalf("opp=%v: chose %d (visits %v, Q %v), want %d", opp, c, tr.Visits, tr.Q, want)
		}
		if opp && (st.OppExpanded != 1 || st.OppPoints != tr.Visits[0]-1) {
			t.Fatalf("opp: expanded %d, opponent points %d, want 1 and a's visits past its expansion (%d)", st.OppExpanded, st.OppPoints, tr.Visits[0]-1)
		}
		if !opp && (st.OppExpanded != 0 || st.OppPoints != 0) {
			t.Fatalf("own: opponent counters %d/%d", st.OppExpanded, st.OppPoints)
		}
		t.Logf("opp=%v: visits %v, Q %v", opp, tr.Visits, tr.Q)
	}
}

// Availability at an opponent's point is the ISMCTS rule of the seat's own
// points: the worlds alternate between offering the opponent {x, y} and
// {x, z} (a re-dealt hand), each reply is available exactly in the worlds
// that offer it, is only selected there, and the one missing per visit is
// counted unavailable. The opponent prefers z (the seat's 0) where offered,
// x (0.5) over y (1) elsewhere.
func TestOpponentPointAvailability(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":     {keys: []Key{"a"}, prior: []float64{1}, value: 0.5},
			"/a":   {value: 0.5, opp: true},
			"/a/x": {value: 0.5, terminal: true},
			"/a/y": {value: 1, terminal: true},
			"/a/z": {value: 0, terminal: true},
		},
		offerAt: func(sim int, path string) []Key {
			if path != "/a" {
				return nil
			}
			if sim%2 == 1 {
				return []Key{"x", "z"}
			}
			return []Key{"x", "y"}
		},
	}
	top := newNode(rootOf(g))
	src := &fakeSource{g: g}
	var st Stats
	const sims = 101
	for i := 0; i < sims; i++ {
		env, err := src.Env(i)
		if err != nil {
			t.Fatal(err)
		}
		if err := simulate(top, env, treeOpts(sims), &st); err != nil {
			t.Fatal(err)
		}
	}
	opp := top.child("a").next
	if opp == nil {
		t.Fatal("the opponent's point was never expanded")
	}
	x, y, z := opp.child("x"), opp.child("y"), opp.child("z")
	if x == nil || y == nil || z == nil {
		t.Fatalf("children %v", opp.kids)
	}
	// Sim 0 expands the point; sims 1..100 select there, odd ones offered z.
	if x.avail != 100 || y.avail != 50 || z.avail != 50 {
		t.Fatalf("avail x %d y %d z %d, want 100 50 50", x.avail, y.avail, z.avail)
	}
	if x.n+y.n+z.n != 100 || y.n > y.avail || z.n > z.avail || z.n <= y.n {
		t.Fatalf("visits x %d y %d z %d", x.n, y.n, z.n)
	}
	if st.Unavailable != 100 || st.OppPoints != 100 || st.OppExpanded != 1 {
		t.Fatalf("unavailable %d, opponent points %d, expanded %d; want 100, 100, 1", st.Unavailable, st.OppPoints, st.OppExpanded)
	}
	t.Logf("visits x %d y %d z %d", x.n, y.n, z.n)
}

// checkOppSearch asserts a Search with OpponentNodes on built a tree with
// no failure and with opponent points in it, and returns its counters.
func checkOppSearch(t *testing.T, res Result, sims int) Stats {
	t.Helper()
	st := res.Stats
	if st.Searched != 1 || st.Simulations != sims || st.Completed != sims || st.AllFailed != 0 {
		t.Fatalf("stats %+v, want %d completed simulations", st, sims)
	}
	if st.Panics+st.SubmitErrors+st.BadWorlds+st.NoWorld+st.ChanceFailures != 0 {
		t.Fatalf("failures: panics %d, submit %d, bad world %d, no world %d, chance %d",
			st.Panics, st.SubmitErrors, st.BadWorlds, st.NoWorld, st.ChanceFailures)
	}
	if st.Terminal+st.StepCapped+st.Expanded != st.Completed {
		t.Fatalf("terminal %d + capped %d + expanded %d != completed %d", st.Terminal, st.StepCapped, st.Expanded, st.Completed)
	}
	if st.OppExpanded > st.Expanded {
		t.Fatalf("opponent points expanded %d of %d", st.OppExpanded, st.Expanded)
	}
	for _, k := range res.Keys {
		if strings.HasPrefix(string(k), oppKeyPrefix) {
			t.Fatalf("root key %q is an opponent's", k)
		}
	}
	return st
}

// Search with the opponent in the tree runs over honest redealt worlds --
// a different opponent hand in every simulation -- with no failure, expands
// opponent points, and stays deterministic in its seed, with no network and
// with one (the prior on the opponent's own view). With the option off the
// opponent counters stay 0.
func TestSearchWithOpponentNodesOnRedealtWorlds(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	net := policynet.NewModel(policynet.TableRows, 4, 3, rand.New(rand.NewPCG(51, 52)))
	net.InitValue(2, rand.New(rand.NewPCG(53, 54)))
	const sims = 96
	var oppTotal Stats
	for _, minTurn := range []int32{3, 5, 7} {
		p := feedPosition(t, cfg, minTurn, 2, nil)
		for _, m := range []*policynet.Model{nil, net} {
			run := func(opp bool) Result {
				obs := searchprobe.NewCollector(0)
				src, err := NewRedeal(p.input(t, p.e), obs, 23, 0)
				if err != nil {
					t.Fatal(err)
				}
				opts := DefaultOptions()
				opts.Sims, opts.Seed, opts.OpponentNodes = sims, 23, opp
				res, err := Search(context.Background(), Root{Engine: p.e, Decision: p.d, Bot: p.bot, Observer: obs}, src, m, opts)
				if err != nil {
					t.Fatal(err)
				}
				return res
			}
			off := run(false)
			if off.Stats.OppPoints != 0 || off.Stats.OppExpanded != 0 {
				t.Fatalf("turn %d: opponent counters %d/%d with the option off", minTurn, off.Stats.OppPoints, off.Stats.OppExpanded)
			}
			on := run(true)
			st := checkOppSearch(t, on, sims)
			if again := run(true); sameResult(on, again) != "" || again.Stats != on.Stats {
				t.Fatalf("turn %d: not deterministic: %s", minTurn, sameResult(on, again))
			}
			oppTotal.Add(st)
			t.Logf("turn %d net=%v kind %s: visits off %v on %v; expanded %d (opponent %d), opponent selections %d, prior fallbacks %d",
				minTurn, m != nil, on.Kind, off.Visits, on.Visits, st.Expanded, st.OppExpanded, st.OppPoints, st.PriorFallbacks)
		}
	}
	if oppTotal.OppExpanded == 0 {
		t.Fatal("no search expanded an opponent's point")
	}
}

// The node cache stays exact with opponent points in the tree: a cached
// search over a fixed world -- the clairvoyant clone, the fixed-chance PIMC
// world -- returns the uncached Result, its opponent counters included
// (the stored states carry copies of the opponent's collectors).
func TestNodeCacheIsExactWithOpponentNodes(t *testing.T) {
	roots := measureRoots(t, 6)
	if len(roots) < 4 {
		t.Fatalf("only %d searchable roots", len(roots))
	}
	oppExpanded := 0
	for i, r := range roots {
		for _, kind := range []string{"clairvoyant", "pimc"} {
			run := func(cache int) Result {
				opts := DefaultOptions()
				opts.Sims, opts.Seed, opts.NodeCache, opts.OpponentNodes = 120, uint64(i)*5+3, cache, true
				obs := searchprobe.NewCollector(r.d.Player)
				res, err := Search(context.Background(), Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}, measureSource(kind, r.e, obs), nil, opts)
				if err != nil {
					t.Fatal(err)
				}
				return res
			}
			off := run(0)
			checkOppSearch(t, off, 120)
			oppExpanded += off.Stats.OppExpanded
			for _, cache := range []int{1, 3, DefaultNodeCache} {
				on := run(cache)
				if d := sameResult(off, on); d != "" {
					t.Fatalf("%s %s cache %d: the cached search differs: %s", r.name, kind, cache, d)
				}
			}
		}
	}
	if oppExpanded == 0 {
		t.Fatal("no root expanded an opponent's point")
	}
}

// An az seat with the opponent in its tree plays whole games on honest
// redealt worlds with no failed simulation and replays exactly.
func TestSeatWithOpponentNodesPlaysAndReplays(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	sc := DefaultSeatConfig()
	sc.Search.Sims, sc.World, sc.Search.OpponentNodes = 8, WorldRedeal, true
	var diags []Diag
	prev := Watch
	Watch = func(d Diag) { diags = append(diags, d) }
	t.Cleanup(func() { Watch = prev })
	heads := make([]string, 2)
	var total Stats
	for i := range heads {
		diags = diags[:0]
		az, err := NewSeat(cfg.Seed^1, nil, sc)
		if err != nil {
			t.Fatal(err)
		}
		if heads[i], err = playAZ(t, cfg, az, 400); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			for _, d := range diags {
				total.Add(d.Stats)
			}
		}
	}
	if heads[0] != heads[1] {
		t.Fatalf("replay diverged: %s vs %s", heads[0], heads[1])
	}
	t.Logf("%d searched; completed %d/%d; expanded %d (opponent %d), opponent selections %d; no-world %d, refused %d",
		total.Searched, total.Completed, total.Simulations, total.Expanded, total.OppExpanded, total.OppPoints, total.NoWorld, total.RedealRefused)
	if total.Searched == 0 || total.OppExpanded == 0 {
		t.Fatal("the seat never expanded an opponent's point")
	}
	if total.Panics+total.SubmitErrors+total.BadWorlds+total.ChanceFailures != 0 {
		t.Fatalf("failed simulations: panics %d, submit %d, bad world %d, chance %d", total.Panics, total.SubmitErrors, total.BadWorlds, total.ChanceFailures)
	}
}
