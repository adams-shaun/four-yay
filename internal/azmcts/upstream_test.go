package azmcts

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// --- Options.ParentVisits ----------------------------------------------------

// upNode is one node of upstreamTree, a line-by-line model of upstream
// MageZero's offline MCTS (MCTSNode.java, MCTSNode2.java,
// ComputerPlayerMCTS2.applyMCTS) over the fake game, on upstream's [-1, 1]
// scale: select is MCTSNode.select (sign by mover, unvisited q 0,
// sqrt(parent visits), strict > keeps the first), a node's evaluation backs
// up 1 + v with no visit (MCTSNode2.evaluate) and the expansion then backs up
// a virtual -1 with one visit (applyMCTS), a terminal backs up +-1 with one
// visit, and backpropagate multiplies by backpropDiscount per parent.
type upNode struct {
	parent   *upNode
	path     string
	kids     []*upNode
	prior    float64
	visits   int
	score    float64
	opp      bool
	expanded bool
}

type upstreamTree struct {
	g     *fakeGame
	c     float64
	disc  float64
	root  *upNode
	fixed bool
}

func (u *upstreamTree) backprop(nd *upNode, result float64, n int) {
	for ; nd != nil; nd = nd.parent {
		nd.visits += n
		nd.score += result
		result *= u.disc
	}
}

func (u *upstreamTree) expand(nd *upNode) {
	f := u.g.nodes[nd.path]
	for i, k := range f.keys {
		p := nd.path + "/" + string(k)
		nd.kids = append(nd.kids, &upNode{parent: nd, path: p, prior: f.prior[i], opp: u.g.nodes[p].opp})
	}
	nd.expanded = true
}

func (u *upstreamTree) selectChild(nd *upNode) *upNode {
	sign := 1.0
	if nd.opp {
		sign = -1
	}
	sqrtN := math.Sqrt(float64(nd.visits))
	var best *upNode
	bestVal := math.Inf(-1)
	for _, k := range nd.kids {
		q := 0.0
		if k.visits > 0 {
			q = k.score / float64(k.visits)
		}
		val := sign*q + u.c*k.prior*(sqrtN/float64(1+k.visits))
		if val > bestVal {
			best, bestVal = k, val
		}
	}
	return best
}

func newUpstreamTree(g *fakeGame, c, disc float64) *upstreamTree {
	u := &upstreamTree{g: g, c: c, disc: disc, root: &upNode{opp: g.nodes[""].opp}}
	u.expand(u.root)
	u.backprop(u.root, 1+(2*g.nodes[""].value-1), 0)
	return u
}

func (u *upstreamTree) simulate() {
	cur := u.root
	for cur.expanded {
		cur = u.selectChild(cur)
	}
	f := u.g.nodes[cur.path]
	if f.terminal || f.capped {
		// A walk that ends: upstream's terminal node backs up its result
		// with a visit every time it is selected (a capped fake walk is
		// gorge's evaluated end, the same shape with value v).
		u.backprop(cur, 2*f.value-1, 1)
		return
	}
	u.backprop(cur, 1+(2*f.value-1), 0)
	u.expand(cur)
	u.backprop(cur, -1, 1)
}

// sameUpstream reports where gorge's tree nd and the reference's un differ
// in shape or visits, or where a node's mean value differs beyond tol.
func sameUpstream(nd *node, un *upNode, path string, tol float64) string {
	if nd.n != un.visits && !(un.parent == nil && nd.n == un.visits+1) {
		return fmt.Sprintf("%s: visits %d vs upstream %d", path, nd.n, un.visits)
	}
	for _, k := range nd.kids {
		var uk *upNode
		for _, x := range un.kids {
			if x.path == path+"/"+string(k.key) {
				uk = x
			}
		}
		if uk == nil {
			return fmt.Sprintf("%s/%s: no upstream child", path, k.key)
		}
		if k.n != uk.visits {
			return fmt.Sprintf("%s/%s: visits %d vs upstream %d", path, k.key, k.n, uk.visits)
		}
		if k.n > 0 {
			if got, want := 2*k.w/float64(k.n)-1, uk.score/float64(uk.visits); math.Abs(got-want) > tol {
				return fmt.Sprintf("%s/%s: mean %v vs upstream %v", path, k.key, got, want)
			}
		}
		if k.next != nil {
			if d := sameUpstream(k.next, uk, path+"/"+string(k.key), tol); d != "" {
				return d
			}
		}
	}
	return ""
}

// endGame is g with every terminal value made a win or a loss (upstream's
// terminal result is +-1, MCTSNode.isWinner).
func endGame(g *fakeGame, rng *rand.Rand) *fakeGame {
	for _, p := range sortedPaths(g) {
		n := g.nodes[p]
		if n.terminal {
			n.value = float64(rng.IntN(2))
			g.nodes[p] = n
		}
	}
	return g
}

func sortedPaths(g *fakeGame) []string {
	out := make([]string, 0, len(g.nodes))
	for p := range g.nodes {
		out = append(out, p)
	}
	sortStrings(out)
	return out
}

// upstreamOpts are gorge's knobs for upstream's PUCT: c = C_PUCT / 2 on the
// [0, 1] scale, unvisited children at the mover's 0.5 (upstream's 0), the
// discount per tree edge, and ParentVisits.
func upstreamOpts(sims int, cPUCT, disc float64) Options {
	o := DefaultOptions()
	o.Sims = sims
	o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = cPUCT/2, true, 0.5
	o.Discount, o.DiscountUnit = disc, DiscountAction
	o.ParentVisits = true
	return o
}

// With ParentVisits (and upstream's other knobs) gorge's tree IS upstream's:
// over random fake games, with and without opponent nodes and a discount,
// every node's visits equal the reference model's, every mean value agrees
// (mapped onto [-1, 1]), and RootValue is upstream's root getMeanScore --
// its own evaluation in the sum, not in the count.
func TestParentVisitsReproducesUpstreamMCTS(t *testing.T) {
	differs := 0
	for seed := uint64(1); seed <= 24; seed++ {
		rng := rand.New(rand.NewPCG(seed, seed^0xabc))
		var g *fakeGame
		if seed%2 == 0 {
			g = randomOppGame(rng, 7)
		} else {
			g = randomGame(rng, 7)
		}
		g = endGame(g, rng)
		disc := 1.0
		if seed%3 == 0 {
			disc = 0.97
		}
		for _, sims := range []int{1, 2, 40, 300} {
			u := newUpstreamTree(g, 1, disc)
			for i := 0; i < sims; i++ {
				u.simulate()
			}
			opts := upstreamOpts(sims, 1, disc)
			var st Stats
			top, err := runTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &st)
			if err != nil {
				t.Fatal(err)
			}
			if d := sameUpstream(top, u.root, "", 1e-9); d != "" {
				t.Fatalf("seed %d sims %d: %s", seed, sims, d)
			}
			want := (u.root.score/float64(u.root.visits) + 1) / 2
			if got := rootValue(top, opts); math.Abs(got-want) > 1e-12 {
				t.Fatalf("seed %d sims %d: root value %v, upstream's getMeanScore maps to %v", seed, sims, got, want)
			}
			// The default PUCT reads sqrt(avail + 1): a different tree on
			// most games (the root's first selection already differs).
			opts.ParentVisits = false
			var st2 Stats
			top2, _ := runTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &st2)
			if sims == 300 && sameUpstream(top2, u.root, "", 1e-9) != "" {
				differs++
			}
		}
	}
	if differs == 0 {
		t.Fatal("the default PUCT matched upstream's on every game: the test does not tell them apart")
	}
	t.Logf("24 games x 4 budgets identical to upstream's model; the default PUCT differs on %d of 24 at 300", differs)
}

// Upstream's fresh root has no visit when its first child is chosen
// (MCTSNode2.evaluate backs the root's evaluation up with n = 0), so every
// exploration term is 0 and the first child wins whatever the prior; the
// default reads sqrt(1) and follows the prior.
func TestParentVisitsFirstSelectionIsPriorBlind(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.1, 0.9}, value: 0.5},
		"/a": {value: 0.5, terminal: true},
		"/b": {value: 0.5, terminal: true},
	}}
	for _, tc := range []struct {
		parent bool
		want   []int
	}{{false, []int{0, 1}}, {true, []int{1, 0}}} {
		opts := upstreamOpts(1, 1, 1)
		opts.ParentVisits = tc.parent
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tr.Visits, tc.want) {
			t.Errorf("ParentVisits %v: visits %v, want %v", tc.parent, tr.Visits, tc.want)
		}
	}
}

// Below the root the parent's visits and availability + 1 differ only when a
// child was unavailable in some simulation (a world that did not offer it):
// upstream reads the parent's visits regardless.
func TestParentVisitsReadsTheParentNotAvailability(t *testing.T) {
	nd := &node{n: 9, kids: []*edge{
		{key: "a", prior: 0.5, n: 2, w: 1.2, avail: 8},
		{key: "b", prior: 0.5, n: 0, avail: 0},
	}}
	pt := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	opts := upstreamOpts(1, 1, 1)
	// b: 0.5 + 0.5*0.5*sqrt(9)/1 = 1.25 with the parent's 9 visits;
	// a: 0.6 + 0.5*0.5*3/3 = 0.85. With availability b reads sqrt(1): 0.75.
	if got := selectEdge(nd, pt, opts); got.key != "b" {
		t.Fatalf("ParentVisits chose %s, want b", got.key)
	}
	opts.ParentVisits = false
	if got := selectEdge(nd, pt, opts); got.key != "a" {
		t.Fatalf("availability PUCT chose %s, want a", got.key)
	}
}

// --- Options.DeadlineBestChild -----------------------------------------------

// expiringCtx is a context whose Err turns DeadlineExceeded after n calls:
// RunTree checks it once per simulation, so the "deadline" stops the tree
// after exactly n simulations, with no wall clock.
type expiringCtx struct {
	n     int64
	calls atomic.Int64
}

func (c *expiringCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *expiringCtx) Done() <-chan struct{}       { return nil }
func (c *expiringCtx) Value(any) any               { return nil }
func (c *expiringCtx) Err() error {
	if c.calls.Add(1) > c.n {
		return context.DeadlineExceeded
	}
	return nil
}

// A deadline that stops the tree after n simulations plays upstream's best
// child so far under DeadlineBestChild: exactly the choice, visits and Q of
// a live search of n simulations (the fallback is a pure function of the
// tree), counted in DeadlineHits and DeadlineBest. Without it the bot's
// answer is played; with a context done at the call (no visit) too.
func TestDeadlineBestChildPlaysThePartialTree(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	search := func(ctx context.Context, opts Options) Result {
		obs := searchprobe.NewCollector(d.Player)
		src, err := newTestClairvoyant(e, obs)
		if err != nil {
			t.Fatal(err)
		}
		res, err := Search(ctx, Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, nil, opts)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 40, 3
	opts.DeadlineBestChild = true
	for _, n := range []int64{1, 7, 13} {
		cut := search(&expiringCtx{n: n}, opts)
		live := opts
		live.Sims = int(n)
		want := search(context.Background(), live)
		if cut.Stats.DeadlineHits != 1 || cut.Stats.DeadlineBest != 1 {
			t.Fatalf("n %d: deadline counters %d/%d", n, cut.Stats.DeadlineHits, cut.Stats.DeadlineBest)
		}
		if cut.Choice != want.Choice || !reflect.DeepEqual(cut.Visits, want.Visits) || !reflect.DeepEqual(cut.Q, want.Q) || !reflect.DeepEqual(cut.Intent, want.Intent) {
			t.Fatalf("n %d: the partial tree's move %d %v differs from a live %d-simulation search's %d %v", n, cut.Choice, cut.Visits, n, want.Choice, want.Visits)
		}
		if again := search(&expiringCtx{n: n}, opts); !reflect.DeepEqual(cut, again) {
			t.Fatalf("n %d: two deadline searches with one seed differ", n)
		}
		off := opts
		off.DeadlineBestChild = false
		if r := search(&expiringCtx{n: n}, off); !reflect.DeepEqual(r.Intent, bot) || r.Visits != nil || r.Stats.DeadlineBest != 0 {
			t.Fatalf("n %d: with the switch off the deadline played %+v, want the bot's %+v", n, r.Intent, bot)
		}
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	if r := search(done, opts); !reflect.DeepEqual(r.Intent, bot) || r.Stats.DeadlineBest != 0 || r.Stats.DeadlineHits != 1 {
		t.Fatalf("a deadline before any simulation played %+v (best %d), want the bot's", r.Intent, r.Stats.DeadlineBest)
	}
}

// --- Kinds.Optional, Kinds.Modes ---------------------------------------------

func TestChoiceCandidates(t *testing.T) {
	yesNo := &decision.Decision{Seq: 4, Player: 1, Kind: decision.KTriggerOptional, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "yes", Label: "Yes"}, {Index: 1, Kind: "no", Label: "No"}}}
	got := choiceCandidates(yesNo, decision.Intent{Seq: 4, Player: 1, Choices: []int{1}}, 8)
	if len(got) != 2 || !reflect.DeepEqual(got[0].Choices, []int{1}) || !reflect.DeepEqual(got[1].Choices, []int{0}) {
		t.Fatalf("yes/no candidates %+v, want the bot's no then yes", got)
	}
	modes := &decision.Decision{Seq: 9, Player: 0, Kind: decision.KModes, Min: 2, Max: 2}
	for i := 0; i < 4; i++ {
		modes.Options = append(modes.Options, decision.Option{Index: i, Kind: "mode", Label: fmt.Sprint(i)})
	}
	got = choiceCandidates(modes, decision.Intent{Seq: 9, Player: 0, Choices: []int{3, 1}}, 10)
	var lists [][]int
	for _, in := range got {
		lists = append(lists, in.Choices)
	}
	want := [][]int{{1, 3}, {0, 1}, {0, 2}, {0, 3}, {1, 2}, {2, 3}}
	if !reflect.DeepEqual(lists, want) {
		t.Fatalf("choose-two candidates %v, want %v", lists, want)
	}
	if got := choiceCandidates(modes, decision.Intent{Seq: 9, Player: 0, Choices: []int{3, 1}}, 3); len(got) != 3 {
		t.Fatalf("limit 3 kept %d", len(got))
	}
	if got := choiceCandidates(modes, decision.Intent{Seq: 9, Player: 0, Choices: []int{0}}, 10); got != nil {
		t.Fatalf("a bot answer outside the vocabulary gave candidates %v", got)
	}
	k, err := ParseKinds("optional,modes")
	if err != nil || !k.Optional || !k.Modes || k.Priority {
		t.Fatalf("ParseKinds: %+v %v", k, err)
	}
}

// microPosition plays the default bot from genesis on a spread of repo-deck
// games until a seat faces a searchable decision of kind want, and returns
// the engine there with the bot's answer.
func microPosition(t *testing.T, want decision.Kind) (*rules.Engine, *decision.Decision, decision.Intent) {
	t.Helper()
	decks := [][2]string{{"foundations-calling-all-angels", "foundations-wretched-ranks"}, {"foundations-keen-engineering", "foundations-reign-of-dragons"},
		{"foundations-tramplesaurus-rex", "foundations-calling-all-angels"}, {"mono-green-stompy", "ur-delver"}, {"uw-tempo", "mono-blue-tempo"}}
	for s := uint64(0); s < 24; s++ {
		for _, p := range decks {
			cfg := testConfig(t, p[0], p[1], 70000000+s)
			e := rules.New(cfg)
			e.Advance()
			rngs := searchprobe.BotRandoms(cfg.Seed, 2)
			for steps := 0; steps < 4000 && !e.G.Over; steps++ {
				d := e.Pending()
				if d == nil {
					break
				}
				in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, rngs[d.Player])
				if d.Kind == want && len(d.Options) >= 2 {
					if _, _, ok := enumerate(searchprobe.NewCollector(d.Player), e, d, in, Kinds{Optional: true, Modes: true}, 8); ok {
						return e, d, in
					}
				}
				if err := e.Submit(in); err != nil {
					break
				}
			}
		}
	}
	t.Skipf("no %s decision in the scanned games", want)
	return nil, nil, decision.Intent{}
}

// An optional trigger and a modal choice are searched under their kinds:
// the candidates are their answers, the bot's first, the Result plays a
// valid one, the counters split them out of KindSearched, and the search is
// deterministic. Without the kinds the same decision is not searched.
func TestOptionalAndModesAreSearched(t *testing.T) {
	for _, tc := range []struct {
		kind  decision.Kind
		name  string
		kinds Kinds
	}{{decision.KTriggerOptional, "optional", Kinds{Optional: true}}, {decision.KModes, "modes", Kinds{Modes: true}}} {
		t.Run(tc.name, func(t *testing.T) {
			e, d, bot := microPosition(t, tc.kind)
			opts := DefaultOptions()
			opts.Sims, opts.Seed, opts.Kinds = 30, 5, tc.kinds
			res := searchAt(t, e, d, bot, nil, opts)
			if res.Kind != tc.name || res.Stats.Searched != 1 || len(res.Visits) < 2 {
				t.Fatalf("kind %q searched %d visits %v", res.Kind, res.Stats.Searched, res.Visits)
			}
			if res.Stats.KindSearched != ([NumKinds]int{}) || res.Stats.OptionalSearched+res.Stats.ModesSearched != 1 {
				t.Fatalf("counters %+v", res.Stats)
			}
			if err := d.Validate(res.Intent); err != nil {
				t.Fatalf("played %+v: %v", res.Intent, err)
			}
			if again := searchAt(t, e, d, bot, nil, opts); !reflect.DeepEqual(res, again) {
				t.Fatal("two searches with one seed differ")
			}
			opts.Kinds = AllKinds()
			if off := searchAt(t, e, d, bot, nil, opts); off.Stats.Searched != 0 || !reflect.DeepEqual(off.Intent, bot) {
				t.Fatalf("searched without the kind: %+v", off.Stats)
			}
		})
	}
}

// --- Options.CombatSteps -----------------------------------------------------

// combatSearch searches creature step len(prefix) of e's pending
// declaration.
func combatSearch(t *testing.T, e *rules.Engine, d *decision.Decision, bot decision.Intent, prefix []int, opts Options, r *Reuse) Result {
	t.Helper()
	obs := searchprobe.NewCollector(d.Player)
	src, err := newTestClairvoyant(e, obs)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs, Combat: prefix, Reuse: r}, src, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// combatOpts are upstream's knobs with CombatSteps.
func combatOpts(sims int, seed uint64) Options {
	o := upstreamOpts(sims, 1, 0.99)
	o.Seed = seed
	o.Limit, o.AutoPayment, o.UniformPrior, o.NameKeys = BenchCandidateLimit, true, true, true
	o.CombatSteps = true
	return o
}

// An attackers root splits into upstream's per-creature yes/no steps: one
// step per potential attacker in object order, candidates [no, yes] with a
// uniform prior, the yes being the creature's attack on the defending
// player; every step's search is deterministic, and the answers assemble
// into a declaration the engine accepts.
func TestCombatStepsSplitTheAttack(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	n := CombatStepCount(d)
	if n < 1 {
		t.Fatal("no creature step")
	}
	opts := combatOpts(60, 11)
	var answers []int
	var last state.ObjID
	for k := 0; k < n; k++ {
		res := combatSearch(t, e, d, bot, answers, opts, nil)
		if res.Step == nil || res.Step.Index != k || res.Step.Steps != n || res.Step.Block {
			t.Fatalf("step %d: %+v", k, res.Step)
		}
		if res.Step.Obj <= last {
			t.Fatalf("step %d: creature %d after %d, want object order", k, res.Step.Obj, last)
		}
		last = res.Step.Obj
		if len(res.Keys) != 2 || res.Keys[0] != combatNoKey || res.Step.Answers[0] != -1 {
			t.Fatalf("step %d: keys %v answers %v, want [no, yes]", k, res.Keys, res.Step.Answers)
		}
		if yes := d.Options[res.Step.Answers[1]]; yes.Obj != res.Step.Obj || yes.Battle != 0 {
			t.Fatalf("step %d: yes is option %+v", k, yes)
		}
		if res.Prior[0] != 0.5 || res.Prior[1] != 0.5 {
			t.Fatalf("step %d: prior %v", k, res.Prior)
		}
		if res.Stats.CombatSteps != 1 || res.Stats.KindSearched[KindAttackers] != 1 || res.Stats.Completed != 60 {
			t.Fatalf("step %d: stats %+v", k, res.Stats)
		}
		if again := combatSearch(t, e, d, bot, answers, opts, nil); !reflect.DeepEqual(res, again) {
			t.Fatalf("step %d: two searches with one seed differ", k)
		}
		answers = append(answers, res.StepAnswer())
	}
	in, err := CombatDeclaration(e, d, answers)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Clone().Submit(in); err != nil {
		t.Fatalf("the assembled declaration %+v: %v", in, err)
	}
	if _, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: searchprobe.NewCollector(d.Player), Combat: make([]int, n)}, nil, nil, opts); err == nil {
		t.Fatal("a prefix past the last creature was accepted")
	}
	noSteps := opts
	noSteps.CombatSteps = false
	if _, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: searchprobe.NewCollector(d.Player), Combat: []int{-1}}, nil, nil, noSteps); err == nil {
		t.Fatal("Root.Combat without CombatSteps was accepted")
	}
}

// A blockers root splits into per-blocker steps: [no block, one answer per
// attacker the creature may block], in object order, and the answers
// assemble into a declaration the engine accepts.
func TestCombatStepsSplitTheBlock(t *testing.T) {
	pairs := [][2]string{{"ur-delver", "mono-green-stompy"}, {"mono-blue-tempo", "mono-red-prowess"}, {"uw-tempo", "mono-white-equipment"}}
	for i := 0; i < 36; i++ {
		p := pairs[i%len(pairs)]
		cfg := testConfig(t, p[0], p[1], testSeed+uint64(i))
		e, d, bot, err := findPosition(cfg, decision.KBlockers, 0, 4000)
		if err != nil || CombatStepCount(d) < 1 {
			continue
		}
		opts := combatOpts(40, 13)
		var answers []int
		for k := 0; k < CombatStepCount(d); k++ {
			res := combatSearch(t, e, d, bot, answers, opts, nil)
			if res.Step == nil || !res.Step.Block || res.Keys[0] != combatNoBlockKey || res.Step.Answers[0] != -1 || len(res.Keys) < 2 {
				t.Fatalf("step %d: %+v keys %v", k, res.Step, res.Keys)
			}
			for _, a := range res.Step.Answers[1:] {
				if o := d.Options[a]; o.Obj != res.Step.Obj || o.Attacker == 0 {
					t.Fatalf("step %d: answer option %+v", k, o)
				}
			}
			answers = append(answers, res.StepAnswer())
		}
		in, err := CombatDeclaration(e, d, answers)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.Clone().Submit(in); err != nil {
			t.Fatalf("the assembled declaration %+v: %v", in, err)
		}
		return
	}
	t.Skip("no seat-0 block in the scanned games")
}

// A split declaration the engine would refuse is repaired by its own rules:
// a creature that must attack attacks even when its step said no.
func TestCombatDeclarationRepairsARequiredAttacker(t *testing.T) {
	d := &decision.Decision{Seq: 3, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "attacker", Obj: 7, Player: 1, Required: true},
		{Index: 1, Kind: "attacker", Obj: 9, Player: 1},
	}}
	in, err := assembleCombat(d, []int{-1, 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sortedInts(in.Choices), []int{0, 1}) {
		t.Fatalf("declaration %v, want the required creature added", in.Choices)
	}
}

func sortedInts(xs []int) []int {
	out := append([]int(nil), xs...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// countCombatNodes counts the tree's creature-step nodes below the root.
func countCombatNodes(top *node) int {
	n := 0
	queue := []*node{top}
	for len(queue) > 0 {
		nd := queue[0]
		queue = queue[1:]
		if nd != top && nd.pt != nil && len(nd.pt.Keys) > 0 && (nd.pt.Keys[0] == combatNoKey || nd.pt.Keys[0] == combatNoBlockKey) {
			n++
		}
		for _, k := range nd.kids {
			if k.next != nil {
				queue = append(queue, k.next)
			}
		}
	}
	return n
}

// With every upstream switch on -- ParentVisits, CombatSteps, the micro
// kinds, opponent nodes, and DeadlineBestChild under a live context -- the
// same seed gives a byte-identical Result, the node cache (every cap) stays
// exact, and the trees really hold creature steps in the walk.
func TestUpstreamSwitchesDeterministicAndCacheExact(t *testing.T) {
	roots := measureRoots(t, 8)
	sims := 120
	if testing.Short() {
		roots, sims = roots[:3], 50
	}
	steps, resumes := 0, 0
	for i, r := range roots {
		for _, kind := range []string{"clairvoyant", "pimc"} {
			run := func(cache int) (Result, int) {
				o := combatOpts(sims, uint64(i)*31+3)
				o.Kinds.Optional, o.Kinds.Modes = true, true
				o.OpponentNodes, o.DeadlineBestChild = true, true
				o.NodeCache = cache
				var top *node
				searchTreeHook = func(n *node) { top = n }
				defer func() { searchTreeHook = nil }()
				obs := searchprobe.NewCollector(r.d.Player)
				root := Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}
				root.Macros, root.BotKey = landMacros(r.d, r.bot)
				if r.d.Kind == decision.KAttackers || r.d.Kind == decision.KBlockers {
					root.Macros, root.BotKey = nil, ""
				}
				res, err := Search(context.Background(), root, measureSource(kind, r.e, obs), nil, o)
				if err != nil {
					t.Fatalf("%s %s: %v", r.name, kind, err)
				}
				c := 0
				if top != nil && cache > 0 {
					c = countCombatNodes(top)
				}
				return res, c
			}
			off, _ := run(0)
			if again, _ := run(0); !reflect.DeepEqual(off, again) {
				t.Fatalf("%s %s: two searches with one seed differ", r.name, kind)
			}
			for _, cache := range []int{1, 3, 17, DefaultNodeCache} {
				on, c := run(cache)
				if d := sameResult(off, on); d != "" {
					t.Fatalf("%s %s cache %d: the cached search differs: %s", r.name, kind, cache, d)
				}
				if cache == DefaultNodeCache {
					steps += c
				}
				resumes += on.Stats.NodeResumes
			}
		}
	}
	if steps == 0 || resumes == 0 {
		t.Fatalf("never exercised: %d creature-step nodes, %d resumes", steps, resumes)
	}
	t.Logf("%d roots x 2 sources: deterministic and cache-exact with every upstream switch; %d creature-step nodes, %d resumes", len(roots), steps, resumes)
}

// Tree reuse carries a creature step's subtree into the next creature's
// search: step k+1's root is the node below step k's played answer.
func TestCombatStepsReuseTheNextStep(t *testing.T) {
	pairs := [][2]string{{"mono-green-stompy", "ur-delver"}, {"mono-red-prowess", "mono-blue-tempo"}, {"mono-white-equipment", "uw-tempo"}}
	for i := 0; i < 36; i++ {
		p := pairs[i%len(pairs)]
		cfg := testConfig(t, p[0], p[1], testSeed+uint64(i))
		e, d, bot, err := findPosition(cfg, decision.KAttackers, 4, 4000)
		if err != nil || CombatStepCount(d) < 2 {
			continue
		}
		seed := cfg.Seed
		opts := combatOpts(80, 21)
		opts.ReuseTree = true
		r := NewReuse()
		first := combatSearch(t, e, d, bot, nil, opts, r)
		second := combatSearch(t, e, d, bot, []int{first.StepAnswer()}, opts, r)
		if second.Stats.ReuseHits != 1 || second.Stats.ReuseCarried == 0 {
			t.Fatalf("seed %d: the second step did not reuse the first's subtree: %+v", seed, second.Stats)
		}
		// The carried subtree's keys are the walks': every known child is
		// offered again (a fixed world never makes one unavailable).
		if second.Stats.Unavailable != 0 || first.Stats.Unavailable != 0 {
			t.Fatalf("seed %d: %d / %d known children unavailable: the carried keys do not match the walk's", seed, first.Stats.Unavailable, second.Stats.Unavailable)
		}
		// Upstream's budget: the carried root's own expansion visit counts.
		sum := 0
		for _, v := range second.Visits {
			sum += v
		}
		if second.Stats.ReuseCarried < opts.Sims && second.Stats.Completed == second.Stats.Simulations && sum+1 != opts.Sims {
			t.Fatalf("seed %d: root visits %d + 1, want the budget %d", seed, sum, opts.Sims)
		}
		return
	}
	t.Skip("no attack with two creatures in the scanned games")
}
