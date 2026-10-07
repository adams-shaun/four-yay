package azmcts

import (
	"context"
	"math"
	"reflect"
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// hashScorer is a policynet.External test double: every option scores a
// fixed function of its encoded identity (its slot and hashed rows, never
// its position), so the prior ranks candidates in an order unrelated to
// the enumeration's; every state is worth 0.5.
type hashScorer struct{}

func (hashScorer) Score(_ policynet.State, opts []policynet.Option) []float32 {
	out := make([]float32, len(opts))
	for i, o := range opts {
		h := uint64(14695981039346656037)
		mix := func(x uint64) {
			h ^= x
			h *= 1099511628211
		}
		for _, f := range o.Slots {
			mix(uint64(f.Row))
		}
		mix(0xff)
		for _, f := range o.Hashed {
			mix(uint64(f.Row))
		}
		out[i] = float32(h%1000)/250 - 2 // [-2, 2)
	}
	return out
}

func (hashScorer) Value(policynet.State) float32 { return 0.5 }

func hashNet() *policynet.Model { return policynet.NewExternal(hashScorer{}, policynet.FeaturesMZ) }

// keysOf is cands' keys.
func keysOf(cands []cand) []Key {
	out := make([]Key, len(cands))
	for i, c := range cands {
		out[i] = c.key
	}
	return out
}

// The cut keeps candidate 0 whatever its prior, then the k-1 others of
// highest prior in descending order, ties to the lower index, and
// renormalises; with nothing to drop it keeps every candidate and the
// prior's values.
func TestPriorTopKKeepsTheBotAndTheBestPriors(t *testing.T) {
	cands := make([]cand, 6)
	for i := range cands {
		cands[i].key = Key(rune('a' + i))
	}
	prior := []float64{0.05, 0.1, 0.3, 0.1, 0.25, 0.2}
	kept, p, before := priorTopK(cands, prior, 3)
	if want := []Key{"a", "c", "e"}; !reflect.DeepEqual(keysOf(kept), want) || before != 6 {
		t.Fatalf("k 3: kept %v (before %d), want %v (before 6)", keysOf(kept), before, want)
	}
	for i, want := range []float64{0.05 / 0.6, 0.3 / 0.6, 0.25 / 0.6} {
		if !near(p[i], want) {
			t.Fatalf("k 3: prior %v, want the kept prior renormalised", p)
		}
	}
	// b and d tie at 0.1: the lower index is kept first.
	kept, _, _ = priorTopK(cands, prior, 5)
	if want := []Key{"a", "c", "e", "f", "b"}; !reflect.DeepEqual(keysOf(kept), want) {
		t.Fatalf("k 5: kept %v, want %v", keysOf(kept), want)
	}
	kept, p, before = priorTopK(cands, prior, 6)
	if want := []Key{"a", "c", "e", "f", "b", "d"}; !reflect.DeepEqual(keysOf(kept), want) || before != 0 {
		t.Fatalf("k 6: kept %v (before %d), want every candidate reordered %v (before 0)", keysOf(kept), before, want)
	}
	if !reflect.DeepEqual(p, []float64{0.05, 0.3, 0.25, 0.2, 0.1, 0.1}) {
		t.Fatalf("k 6: prior %v, want the values unchanged", p)
	}
	// A uniform prior (a fallback) keeps the enumeration's first k.
	kept, _, _ = priorTopK(cands, uniform(6), 3)
	if want := []Key{"a", "b", "c"}; !reflect.DeepEqual(keysOf(kept), want) {
		t.Fatalf("uniform: kept %v, want %v", keysOf(kept), want)
	}
	if cands[1].key != "b" || prior[1] != 0.1 {
		t.Fatal("priorTopK modified its inputs")
	}
}

// wantTopK is the cut computed independently of priorTopK: the full
// enumeration's candidate 0, then the k-1 others of highest ranking prior.
func wantTopK(full []cand, prior []float64, k int) []Key {
	idx := make([]int, 0, len(full))
	for i := 1; i < len(full); i++ {
		idx = append(idx, i)
	}
	sort.SliceStable(idx, func(a, b int) bool { return prior[idx[a]] > prior[idx[b]] })
	out := []Key{full[0].key}
	for _, i := range idx[:min(len(idx), k-1)] {
		out = append(out, full[i].key)
	}
	return out
}

// Search under PriorTopK with a network: the root's candidates are the
// bot's plus the K-1 others of highest network prior over the FULL
// enumeration -- not the enumeration's first K -- with the bot's answer
// still candidate 0 and the Result's lists aligned. Over several roots at
// least one cut must differ from the Limit-style first-K cut.
func TestSearchPriorTopKKeepsTheNetworksBest(t *testing.T) {
	const k = 3
	net := hashNet()
	roots := measureRoots(t, 10)
	differs, cuts, inWalk := 0, 0, 0
	for i, r := range roots {
		full, kind, _, ok, _ := enumerateCut(searchprobe.NewCollector(r.d.Player), r.e, r.d, r.bot, AllKinds(), BenchCandidateLimit, false)
		if !ok {
			t.Fatalf("%s: no candidates", r.name)
		}
		prior, fell := priorsWith(net, r.e, r.d, r.bot, kind, full, nil, true)
		if fell {
			t.Fatalf("%s: the ranking prior fell back", r.name)
		}
		want := wantTopK(full, prior, k)
		if len(full) > k && !reflect.DeepEqual(want, keysOf(full[:k])) {
			differs++
		}
		opts := DefaultOptions()
		opts.Sims, opts.Seed, opts.PriorTopK, opts.HeuristicLeaf = 24, uint64(i)+1, k, true
		res := searchAt(t, r.e, r.d, r.bot, net, opts)
		checkResult(t, r.d, r.bot, res, opts.Sims)
		if !reflect.DeepEqual(res.Keys, want) {
			t.Fatalf("%s: kept %v, want %v", r.name, res.Keys, want)
		}
		if !sameIntent(res.Candidates[0], r.bot) {
			t.Fatalf("%s: candidate 0 %+v is not the bot's answer %+v", r.name, res.Candidates[0], r.bot)
		}
		byKey := make(map[Key]decision.Intent, len(full)) // lookup only
		for _, c := range full {
			byKey[c.key] = c.in
		}
		for j, in := range res.Candidates {
			if !reflect.DeepEqual(in, byKey[res.Keys[j]]) {
				t.Fatalf("%s: candidate %d is not its key's intent: %+v vs %+v", r.name, j, in, byKey[res.Keys[j]])
			}
		}
		st := res.Stats
		if st.PriorTopKPoints < 1 || (len(full) > k && st.PriorTopKCuts < 1) {
			t.Fatalf("%s: %d candidates, top-k points %d cuts %d", r.name, len(full), st.PriorTopKPoints, st.PriorTopKCuts)
		}
		if len(full) > k {
			cuts++
		}
		inWalk += st.PriorTopKPoints - 1
	}
	if differs == 0 || cuts == 0 || inWalk == 0 {
		t.Fatalf("the roots never exercised the cut: %d differ from the first-k cut, %d cut, %d in-walk ranked points", differs, cuts, inWalk)
	}
	t.Logf("%d roots: %d cut, %d differ from the first-%d cut; %d in-walk ranked points", len(roots), cuts, differs, k, inWalk)
}

// topKWalk walks one clairvoyant world from root r under top-k k, always
// playing each point's candidate 0 (the bot's answer: the same walk
// whatever k is), and returns the points it reached.
func topKWalk(t *testing.T, r measureRoot, net *policynet.Model, k int, autoPay bool, steps int) ([]*Point, Stats) {
	t.Helper()
	obs := searchprobe.NewCollector(r.d.Player)
	full, kind, _, ok, _ := enumerateCut(obs, r.e, r.d, r.bot, AllKinds(), BenchCandidateLimit, autoPay)
	if !ok {
		t.Fatalf("%s: no candidates", r.name)
	}
	cands, prior, _, _ := rankedPrior(net, k, r.e, r.d, r.bot, kind, full, nil)
	var st Stats
	cfg := &walkConfig{
		net: net, kinds: AllKinds(), limit: BenchCandidateLimit, priorTopK: k, maxSteps: 2000,
		envSeed: 0x70707, actor: r.d.Player, autoPayment: autoPay, rootRefs: obs.Introduced(),
		root: &Point{Keys: keysOf(cands), Prior: prior}, rootCands: cands, rootDec: r.d, stats: &st,
	}
	src, err := newTestClairvoyant(r.e, obs)
	if err != nil {
		t.Fatal(err)
	}
	w, err := src.World(0)
	if err != nil {
		t.Fatal(err)
	}
	env, err := newEngineEnv(w, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var pts []*Point
	pt := env.Root()
	for i := 0; i < steps && pt != nil; i++ {
		if pt, err = env.Play(pt.Keys[0]); err != nil {
			t.Fatalf("%s: step %d: %v", r.name, i, err)
		}
		if pt != nil {
			pts = append(pts, pt)
		}
	}
	return pts, st
}

// In the walk every searched point is cut the root's way: walked with a
// small k, each point is the first k candidates of the same point walked
// with nothing cut (every candidate ranked by prior), its prior that
// point's renormalised -- under both payment vocabularies.
func TestPriorTopKCutsEveryInWalkPoint(t *testing.T) {
	const k = 2
	net := hashNet()
	var checked [2]int
	for a, auto := range []bool{false, true} {
		roots := measureRoots(t, 6)
		if auto {
			roots = autoPayRoots(t, 4)
		}
		for _, r := range roots {
			small, sst := topKWalk(t, r, net, k, auto, 40)
			all, ast := topKWalk(t, r, net, BenchCandidateLimit, auto, 40)
			if len(small) != len(all) {
				t.Fatalf("auto %v %s: the walks reached %d and %d points", auto, r.name, len(small), len(all))
			}
			cut := 0
			for i, p := range small {
				q := all[i]
				n := min(k, len(q.Keys))
				if !reflect.DeepEqual(p.Keys, q.Keys[:n]) {
					t.Fatalf("auto %v %s point %d: kept %v, want the first %d of %v", auto, r.name, i, p.Keys, n, q.Keys)
				}
				sum := 0.0
				for _, x := range q.Prior[:n] {
					sum += x
				}
				for j := range p.Prior {
					if !near(p.Prior[j], q.Prior[j]/sum) {
						t.Fatalf("auto %v %s point %d: prior %v, want %v renormalised", auto, r.name, i, p.Prior, q.Prior[:n])
					}
				}
				for j := 2; j < len(q.Prior); j++ {
					if q.Prior[j] > q.Prior[j-1] {
						t.Fatalf("auto %v %s point %d: the others are not prior-descending: %v", auto, r.name, i, q.Prior)
					}
				}
				if len(q.Keys) > k {
					cut++
					if !p.ranked || p.before != len(q.Keys) {
						t.Fatalf("auto %v %s point %d: ranked %v before %d, want a cut of %d", auto, r.name, i, p.ranked, p.before, len(q.Keys))
					}
				}
			}
			if sst.PriorTopKPoints != len(small) || sst.PriorTopKCuts != cut || ast.PriorTopKCuts != 0 || sst.PriorFallbacks != 0 {
				t.Fatalf("auto %v %s: stats %d points %d cuts %d fallbacks (no-cut walk %d cuts), want %d points %d cuts",
					auto, r.name, sst.PriorTopKPoints, sst.PriorTopKCuts, sst.PriorFallbacks, ast.PriorTopKCuts, len(small), cut)
			}
			checked[a] += cut
		}
	}
	if checked[0] == 0 || checked[1] == 0 {
		t.Fatalf("in-walk points cut: %d manual, %d auto-pay; want some of each", checked[0], checked[1])
	}
	t.Logf("in-walk points cut: %d manual, %d auto-pay", checked[0], checked[1])
}

// autoPayRoots finds up to n seat-0 priority roots the auto-pay bot
// answers inside the auto-pay vocabulary, with at least three candidates
// and a payment cast the engine offers no legacy cast option for (the
// candidates the ordinary prior cannot score).
func autoPayRoots(t *testing.T, n int) []measureRoot {
	t.Helper()
	var out []measureRoot
	for s := uint64(0); len(out) < n && s < 16; s++ {
		cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", 91000000+s)
		e := rules.New(cfg)
		e.Advance()
		rngs := searchprobe.BotRandoms(cfg.Seed, 2)
		board := botpolicy.NewBoard(2)
		for steps := 0; steps < 6000 && !e.G.Over; steps++ {
			d := e.Pending()
			in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
			if d.Player == 0 && d.Kind == decision.KPriority && e.G.Turn >= 3 {
				e.EnsurePaymentActions()
				auto, err := seat.NewBot(7).EnableAutoPayMana().Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
				if err == nil {
					cands, _, _, ok, _ := enumerateCut(searchprobe.NewCollector(0), e, d, auto, AllKinds(), BenchCandidateLimit, true)
					unscored := false
					for _, c := range cands {
						_, scored := c.scoreChoices()
						unscored = unscored || !scored
					}
					if ok && len(cands) >= 3 && unscored {
						out = append(out, measureRoot{name: "autopay", e: e, d: d, bot: auto})
						break
					}
				}
			}
			if err := e.Submit(in); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no auto-pay root with an unscored payment cast")
	}
	return out
}

// Under auto-pay the ordinary prior cannot score a payment cast with no
// legacy option and falls back to uniform; the ranking prior scores it as
// the plain cast it stands for, so the network chooses among the casts.
func TestPriorTopKRanksPaymentCasts(t *testing.T) {
	net := hashNet()
	for _, r := range autoPayRoots(t, 3) {
		full, kind, _, ok, _ := enumerateCut(searchprobe.NewCollector(0), r.e, r.d, r.bot, AllKinds(), BenchCandidateLimit, true)
		if !ok {
			t.Fatal("no candidates")
		}
		if _, fell := priors(net, r.e, r.d, r.bot, kind, full, nil); !fell {
			t.Fatal("the ordinary prior scored a payment cast with no legacy option")
		}
		p, fell := priorsWith(net, r.e, r.d, r.bot, kind, full, nil, true)
		if fell {
			t.Fatal("the ranking prior fell back")
		}
		distinct := map[float64]bool{}
		sum := 0.0
		for _, x := range p {
			distinct[x] = true
			sum += x
		}
		if len(distinct) < 2 || math.Abs(sum-1) > 1e-9 {
			t.Fatalf("ranking prior %v: want a normalised, non-uniform prior", p)
		}
		opts := DefaultOptions()
		opts.Sims, opts.Seed, opts.PriorTopK, opts.AutoPayment, opts.HeuristicLeaf = 16, 9, 2, true, true
		res := searchAt(t, r.e, r.d, r.bot, net, opts)
		checkResult(t, r.d, r.bot, res, opts.Sims)
		if want := wantTopK(full, p, 2); !reflect.DeepEqual(res.Keys, want) {
			t.Fatalf("kept %v, want %v", res.Keys, want)
		}
		if res.Stats.PriorFallbacks != 0 || res.Stats.PriorTopKCuts < 1 {
			t.Fatalf("stats: %d fallbacks, %d top-k cuts", res.Stats.PriorFallbacks, res.Stats.PriorTopKCuts)
		}
	}
}

// PriorTopK is inert without a network prior: with no network, or with
// UniformPrior, the Result is the one without it, byte for byte.
func TestPriorTopKIsInertWithoutANetworkPrior(t *testing.T) {
	roots := measureRoots(t, 4)
	for i, r := range roots {
		for _, uni := range []bool{false, true} {
			var net *policynet.Model
			if uni {
				net = hashNet()
			}
			opts := DefaultOptions()
			opts.Sims, opts.Seed, opts.UniformPrior, opts.HeuristicLeaf = 24, uint64(i)+5, uni, true
			off := searchAt(t, r.e, r.d, r.bot, net, opts)
			opts.PriorTopK = 2
			on := searchAt(t, r.e, r.d, r.bot, net, opts)
			if !reflect.DeepEqual(off, on) {
				t.Fatalf("%s uniform %v: PriorTopK changed a search without a network prior", r.name, uni)
			}
			if on.Stats.PriorTopKPoints != 0 {
				t.Fatalf("%s uniform %v: %d top-k points counted", r.name, uni, on.Stats.PriorTopKPoints)
			}
		}
	}
}

// Under PriorTopK the search stays a pure function of its inputs, and the
// node cache stays exact: the counters a stored point carries (ranked,
// before) are counted again on a cached walk.
func TestPriorTopKIsDeterministicAndCacheExact(t *testing.T) {
	net := hashNet()
	for i, r := range measureRoots(t, 4) {
		run := func(cache int) Result {
			opts := DefaultOptions()
			opts.Sims, opts.Seed, opts.PriorTopK, opts.NodeCache, opts.HeuristicLeaf = 60, uint64(i)+11, 3, cache, true
			return searchAt(t, r.e, r.d, r.bot, net, opts)
		}
		off := run(0)
		if again := run(0); !reflect.DeepEqual(off, again) {
			t.Fatalf("%s: two top-k searches differ", r.name)
		}
		for _, cache := range []int{1, 3, DefaultNodeCache} {
			if d := sameResult(off, run(cache)); d != "" {
				t.Fatalf("%s cache %d: the cached top-k search differs: %s", r.name, cache, d)
			}
		}
	}
}

func TestValidateRefusesAOneCandidateTopK(t *testing.T) {
	for _, k := range []int{-1, 1} {
		opts := DefaultOptions()
		opts.PriorTopK = k
		if err := opts.Validate(nil); err == nil {
			t.Fatalf("PriorTopK %d accepted", k)
		}
	}
	opts := DefaultOptions()
	opts.PriorTopK = 2
	if err := opts.Validate(hashNet()); err != nil {
		t.Fatal(err)
	}
}
