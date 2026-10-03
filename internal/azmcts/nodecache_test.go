package azmcts

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// withoutCost is st with the walk's cost counters zeroed: the only fields
// the node cache may change.
func withoutCost(st Stats) Stats {
	st.EnvSteps = 0
	st.Plays, st.ReplayPlays, st.ReplaySteps = 0, 0, 0
	st.NodeSaves, st.NodeResumes, st.NodeEvicts = 0, 0, 0
	return st
}

// sameResult reports the first difference between two Results, the cost
// counters aside; floats compare bit for bit.
func sameResult(a, b Result) string {
	bits := func(xs []float64) []uint64 {
		out := make([]uint64, len(xs))
		for i, x := range xs {
			out[i] = math.Float64bits(x)
		}
		return out
	}
	switch {
	case a.Kind != b.Kind || a.Choice != b.Choice:
		return fmt.Sprintf("kind/choice %q/%d vs %q/%d", a.Kind, a.Choice, b.Kind, b.Choice)
	case !reflect.DeepEqual(a.Keys, b.Keys) || !reflect.DeepEqual(a.Labels, b.Labels):
		return "keys"
	case !reflect.DeepEqual(a.Avail, b.Avail):
		return fmt.Sprintf("avail %v vs %v", a.Avail, b.Avail)
	case !reflect.DeepEqual(a.Visits, b.Visits):
		return fmt.Sprintf("visits %v vs %v", a.Visits, b.Visits)
	case !reflect.DeepEqual(bits(a.Q), bits(b.Q)):
		return fmt.Sprintf("Q %v vs %v", a.Q, b.Q)
	case !reflect.DeepEqual(bits(a.Prior), bits(b.Prior)):
		return "prior"
	case math.Float64bits(a.RootValue) != math.Float64bits(b.RootValue):
		return fmt.Sprintf("root value %v vs %v", a.RootValue, b.RootValue)
	case !reflect.DeepEqual(a.Candidates, b.Candidates) || !reflect.DeepEqual(a.Intent, b.Intent):
		return "candidates/intent"
	case withoutCost(a.Stats) != withoutCost(b.Stats):
		return fmt.Sprintf("stats\n  %+v\nvs\n  %+v", withoutCost(a.Stats), withoutCost(b.Stats))
	}
	return ""
}

// The node cache is exact: over many roots, both fixed-world sources
// (clairvoyant, fixed-chance PIMC), and caps from "only the root" through
// eviction to room for every node, a search with the cache returns the
// same Result as one without it -- visits, Q, prior, root value, choice
// and every outcome counter -- and only the walk's cost counters differ.
func TestNodeCacheIsExact(t *testing.T) {
	t.Parallel()
	roots := measureRoots(t, 18)
	if len(roots) < 12 {
		t.Fatalf("only %d searchable roots", len(roots))
	}
	sims := 160
	if testing.Short() {
		roots, sims = roots[:4], 60
	}
	// Each root is searched independently, so the roots run as parallel
	// subtests: the package's other search tests are serial, and this one's
	// single-threaded loops left three of the gate cgroup's four CPUs idle
	// on the module gate's critical path. Per-root failures stay attributed
	// to their root; the cross-root assertions wait for every subtest.
	var mu sync.Mutex
	var saves, resumes, evicts, replayOff, replayOn int
	for i, r := range roots {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			var rsaves, rresumes, revicts, rreplayOff, rreplayOn int
			for _, kind := range []string{"clairvoyant", "pimc"} {
				run := func(cache int) Result {
					opts := DefaultOptions()
					opts.Sims, opts.Seed, opts.NodeCache = sims, uint64(i)*7+1, cache
					opts.Noise = i%2 == 1 // root noise on half the roots
					obs := searchprobe.NewCollector(r.d.Player)
					res, err := Search(context.Background(), Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}, measureSource(kind, r.e, obs), nil, opts)
					if err != nil {
						t.Fatal(err)
					}
					return res
				}
				off := run(0)
				if off.Stats.NodeSaves != 0 {
					t.Fatalf("%s %s: the cache ran with NodeCache 0", r.name, kind)
				}
				rreplayOff += off.Stats.ReplaySteps
				for _, cache := range []int{1, 3, 17, DefaultNodeCache} {
					on := run(cache)
					if d := sameResult(off, on); d != "" {
						t.Fatalf("%s %s cache %d: the cached search differs: %s", r.name, kind, cache, d)
					}
					if on.Stats.NodeSaves == 0 {
						t.Fatalf("%s %s cache %d: a fixed world saved no node state", r.name, kind, cache)
					}
					rsaves += on.Stats.NodeSaves
					rresumes += on.Stats.NodeResumes
					revicts += on.Stats.NodeEvicts
					if cache == DefaultNodeCache {
						rreplayOn += on.Stats.ReplaySteps
					}
				}
			}
			mu.Lock()
			saves, resumes, evicts = saves+rsaves, resumes+rresumes, evicts+revicts
			replayOff, replayOn = replayOff+rreplayOff, replayOn+rreplayOn
			mu.Unlock()
		})
	}
	t.Cleanup(func() {
		if resumes == 0 || evicts == 0 {
			t.Fatalf("the roots never exercised the cache: %d resumes, %d evictions", resumes, evicts)
		}
		if replayOn*4 > replayOff {
			t.Fatalf("the cache re-walked %d env steps, the uncached search %d: want at most a quarter", replayOn, replayOff)
		}
		t.Logf("%d roots x 2 sources x 4 caps: identical; %d saves, %d resumes, %d evictions; replayed env steps %d -> %d",
			len(roots), saves, resumes, evicts, replayOff, replayOn)
	})
}

// A source whose worlds differ between simulations never uses the cache:
// a hypothetical clone re-seeded every simulation, and the redeal source.
func TestNodeCacheStaysOffForChangingWorlds(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	obs := searchprobe.NewCollector(0)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 3
	res, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, hypSource{e: e, obs: obs}, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats.Completed == 0 || res.Stats.NodeSaves != 0 || res.Stats.NodeResumes != 0 {
		t.Fatalf("re-seeded worlds: stats %+v, want completed simulations and no node state", res.Stats)
	}
	var rs WorldSource = &RedealSource{}
	if isFixed(rs) {
		t.Fatal("the redeal source declares a fixed world")
	}
}

// botStreams is searchprobe.BotRandoms, draw for draw.
func TestBotStreamsAreBotRandoms(t *testing.T) {
	want := searchprobe.BotRandoms(0xabcdef, 3)
	got, pcgs := botStreams(0xabcdef, 3)
	for i := range want {
		for j := 0; j < 50; j++ {
			if a, b := want[i].Uint64(), got[i].Uint64(); a != b {
				t.Fatalf("stream %d draw %d: %d vs %d", i, j, a, b)
			}
		}
		saved := *pcgs[i]
		a := got[i].Uint64()
		restored := saved
		if b := rand.New(&restored).Uint64(); a != b {
			t.Fatalf("stream %d: a saved PCG does not resume the stream", i)
		}
	}
}

// fakeStates is the fake game as a NodeStateSource: a walk's state is its
// path, so Save and Resume are exact by construction and the tree logic
// alone is under test.
type fakeStates struct{ fakeSource }

func (s *fakeStates) Save(env Env, final bool) (any, error) { return env.(*fakeEnv).path, nil }

func (s *fakeStates) Resume(snap any) (Env, error) {
	return &fakeEnv{g: s.g, path: snap.(string)}, nil
}

// randomGame is a fake game of the given depth: every node offers 2-4
// keys with random priors and values, and some nodes end the walk.
func randomGame(rng *rand.Rand, depth int) *fakeGame {
	g := &fakeGame{nodes: map[string]fakeNode{}}
	var build func(path string, d int)
	build = func(path string, d int) {
		n := fakeNode{value: rng.Float64()}
		if d > 0 && path != "" && rng.IntN(6) == 0 {
			n.terminal = true
		}
		if d == depth {
			n.capped = true
		}
		if !n.terminal && !n.capped {
			k := 2 + rng.IntN(3)
			sum := 0.0
			for i := 0; i < k; i++ {
				n.keys = append(n.keys, Key(fmt.Sprintf("k%d", i)))
				p := 0.1 + rng.Float64()
				n.prior = append(n.prior, p)
				sum += p
			}
			for i := range n.prior {
				n.prior[i] /= sum
			}
		}
		g.nodes[path] = n
		for _, k := range n.keys {
			build(path+"/"+string(k), d+1)
		}
	}
	build("", 0)
	return g
}

// Over the fake env, RunTree with the cache (every cap, eviction included)
// builds the same tree statistics as without it, and skips the re-walks.
func TestNodeCacheTreeMatchesUncached(t *testing.T) {
	for seed := uint64(1); seed <= 20; seed++ {
		g := randomGame(rand.New(rand.NewPCG(seed, seed^7)), 6)
		opts := treeOpts(400)
		opts.NodeCache = 0
		var stOff Stats
		off, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &stOff)
		if err != nil {
			t.Fatal(err)
		}
		for _, cache := range []int{1, 2, 5, 40, 10000} {
			opts.NodeCache = cache
			var st Stats
			src := &fakeStates{fakeSource{g: g}}
			on, err := RunTree(context.Background(), rootOf(g), src, opts, &st)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(off, on) || withoutCost(stOff) != withoutCost(st) {
				t.Fatalf("seed %d cache %d: %+v %+v, want %+v %+v", seed, cache, on, withoutCost(st), off, withoutCost(stOff))
			}
			if src.calls != 1 {
				t.Fatalf("seed %d cache %d: %d worlds asked for, want only the root's", seed, cache, src.calls)
			}
			if cache >= 40 && st.Plays >= stOff.Plays {
				t.Fatalf("seed %d cache %d: %d plays with the cache, %d without", seed, cache, st.Plays, stOff.Plays)
			}
		}
	}
}

// landMacros is a root macro per land play d offers (the full root's way
// of searching a play outside the auto-pay vocabulary), and the key of the
// one bot plays, "" when bot is no land play.
func landMacros(d *decision.Decision, bot decision.Intent) ([]Macro, Key) {
	var out []Macro
	var botKey Key
	for i, o := range d.Options {
		if d.Kind != decision.KPriority || o.Kind != "play_land" {
			continue
		}
		m := Macro{Key: Key(fmt.Sprintf("%sland-%d", MacroKeyPrefix, o.Obj)), Label: "Play " + o.Label, Steps: []MacroStep{{
			Player: d.Player, Kind: d.Kind, Picks: []rules.ScriptPick{{Kind: o.Kind, Obj: o.Obj, Label: o.Label, ManaSymbol: o.ManaSymbol}},
		}}}
		if len(bot.Choices) == 1 && bot.Choices[0] == i {
			botKey = m.Key
		}
		out = append(out, m)
	}
	return out, botKey
}

// The node cache stays exact under the search benchmark's knobs: auto-pay
// candidates (the actor's own auto-pay bot answering its unsearched asks,
// whose stream the cache saves), the full root with macro edges (a land
// play played as one edge), absolute unvisited Q, name keys, the
// benchmark's candidate limit, and the backup discount in every unit (which
// reads each node's engine clock, saved with its state). Everything but
// EnvSteps and the cache's own counters is identical, the leaf-depth sums,
// Truncated and PriorFallbacks included.
func TestNodeCacheIsExactWithBenchKnobs(t *testing.T) {
	t.Parallel()
	roots := measureRoots(t, 10)
	e, d, land := landPlayRoot(t)
	roots = append(roots, measureRoot{name: "land-play", e: e, d: d, bot: land})
	sims := 120
	if testing.Short() {
		roots, sims = append(roots[:3], roots[len(roots)-1]), 50
	}
	type unit struct {
		name  string
		gamma float64
		u     DiscountUnit
	}
	units := []unit{{"none", 0, 0}, {"ply", 0.97, DiscountPly}, {"action", 0.9, DiscountAction}, {"turn", 0.8, DiscountTurn}}
	// Each root is searched independently, so the roots run as parallel
	// subtests: this test is the azmcts package's whole critical path on the
	// module gate, and its serial loop used one of the gate cgroup's four
	// CPUs. Per-root failures stay attributed to their root; the cross-root
	// assertions wait for every subtest.
	var mu sync.Mutex
	var macroVisits, resumes int
	for i, r := range roots {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			macros, botKey := landMacros(r.d, r.bot)
			var rmacroVisits, rresumes int
			for _, kind := range []string{"clairvoyant", "pimc"} {
				for _, un := range units {
					run := func(cache int) Result {
						opts := DefaultOptions()
						opts.Sims, opts.Seed, opts.NodeCache = sims, uint64(i)*11+3, cache
						opts.CPUCT, opts.AbsoluteUnvisitedQ, opts.UnvisitedQ = 0.5, true, 0.5
						opts.Limit, opts.AutoPayment, opts.UniformPrior, opts.NameKeys = BenchCandidateLimit, true, true, true
						opts.Noise, opts.Sample = false, false
						opts.Discount, opts.DiscountUnit = un.gamma, un.u
						obs := searchprobe.NewCollector(r.d.Player)
						root := Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs, Macros: macros, BotKey: botKey}
						res, err := Search(context.Background(), root, measureSource(kind, r.e, obs), nil, opts)
						if err != nil {
							t.Fatal(err)
						}
						return res
					}
					off := run(0)
					if off.Stats.Completed == 0 {
						continue
					}
					for k, key := range off.Keys {
						if IsMacroKey(key) {
							rmacroVisits += off.Visits[k]
						}
					}
					for _, cache := range []int{1, 3, 17, DefaultNodeCache} {
						on := run(cache)
						if diff := sameResult(off, on); diff != "" {
							t.Fatalf("%s %s discount %s cache %d: the cached search differs: %s", r.name, kind, un.name, cache, diff)
						}
						rresumes += on.Stats.NodeResumes
					}
				}
			}
			mu.Lock()
			macroVisits, resumes = macroVisits+rmacroVisits, resumes+rresumes
			mu.Unlock()
		})
	}
	t.Cleanup(func() {
		if macroVisits == 0 || resumes == 0 {
			t.Fatalf("the roots never exercised a macro edge (%d visits) or a resume (%d)", macroVisits, resumes)
		}
		t.Logf("%d roots x 2 sources x %d discounts x 4 caps: identical; %d macro-edge visits, %d resumes", len(roots), len(units), macroVisits, resumes)
	})
}
