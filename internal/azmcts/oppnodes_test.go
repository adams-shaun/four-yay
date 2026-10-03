package azmcts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// The opponent-node tests (Options.OpponentNodes), numbered as the design's
// test list (/mnt/sata/gorge-training/mzrepro/DESIGN-oppnodes.md). Test 5,
// byte-identical when off, is oppnodes_golden_test.go.

// --- 1. Minimax over the fake env ------------------------------------------

// The root's a leads to an opponent node whose children are worth 0.9 (x)
// and 0.1 (y) to the searching seat: the opponent's visits go to y, so a
// is worth about 0.1 to the actor and b (a sure 0.5) wins the root. The same
// tree with the node left to the actor (no opp flag) visits x instead.
func TestOpponentNodeMinimax(t *testing.T) {
	game := func(opp bool) *fakeGame {
		return &fakeGame{nodes: map[string]fakeNode{
			"":     {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
			"/a":   {keys: []Key{"x", "y"}, prior: []float64{0.5, 0.5}, value: 0.5, opp: opp},
			"/a/x": {value: 0.9, terminal: true},
			"/a/y": {value: 0.1, terminal: true},
			"/b":   {value: 0.5, terminal: true},
		}}
	}
	for _, opp := range []bool{true, false} {
		g := game(opp)
		var st Stats
		top, err := runTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(400), &st)
		if err != nil {
			t.Fatal(err)
		}
		a := top.child("a").next
		if a == nil || a.opp != opp {
			t.Fatalf("opp %v: node /a %+v", opp, a)
		}
		x, y := a.child("x").n, a.child("y").n
		res := treeResult(rootOf(g), top)
		if opp {
			if y <= 4*x {
				t.Errorf("opponent node: visits x %d y %d, want the opponent's y", x, y)
			}
			if res.Visits[1] <= res.Visits[0] || choose(res.Visits, false, nil) != 1 {
				t.Errorf("opponent node: root visits %v, want b", res.Visits)
			}
			if st.OppPoints != a.n-1 || st.OppExpanded != 1 {
				t.Errorf("opponent node: OppPoints %d OppExpanded %d, want %d and 1", st.OppPoints, st.OppExpanded, a.n-1)
			}
		} else {
			if x <= 4*y {
				t.Errorf("actor node: visits x %d y %d, want x", x, y)
			}
			if res.Visits[0] <= res.Visits[1] {
				t.Errorf("actor node: root visits %v, want a", res.Visits)
			}
			if st.OppPoints != 0 || st.OppExpanded != 0 {
				t.Errorf("actor node: OppPoints %d OppExpanded %d, want 0", st.OppPoints, st.OppExpanded)
			}
		}
	}
}

// --- 2. Symmetry over the fake env -----------------------------------------

// mirrorGame is g seen from the other seat: every node's owner flipped and
// every value v read as 1 - v.
func mirrorGame(g *fakeGame) *fakeGame {
	out := &fakeGame{nodes: make(map[string]fakeNode, len(g.nodes))}
	for p, n := range g.nodes { // map to map: no order reaches anything
		n.opp = !n.opp
		n.value = 1 - n.value
		out.nodes[p] = n
	}
	return out
}

// randomOppGame is randomGame with each non-root node's owner drawn at
// random.
func randomOppGame(rng *rand.Rand, depth int) *fakeGame {
	g := randomGame(rng, depth)
	paths := make([]string, 0, len(g.nodes))
	for p := range g.nodes {
		paths = append(paths, p)
	}
	sortStrings(paths)
	for _, p := range paths {
		if p == "" {
			continue
		}
		n := g.nodes[p]
		n.opp = rng.IntN(2) == 0
		g.nodes[p] = n
	}
	return g
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// sameTreeMirrored reports the first place two trees differ in shape or
// visits, or where b's values are not 1 - a's (within tol).
func sameTreeMirrored(a, b *node, path string, tol float64) string {
	if a.n != b.n || len(a.kids) != len(b.kids) || a.opp == b.opp {
		return fmt.Sprintf("%s: n %d/%d kids %d/%d opp %v/%v", path, a.n, b.n, len(a.kids), len(b.kids), a.opp, b.opp)
	}
	if math.Abs(a.w-(float64(b.n)-b.w)) > tol*float64(a.n) {
		return fmt.Sprintf("%s: w %v vs mirrored %v", path, a.w, float64(b.n)-b.w)
	}
	for i, ka := range a.kids {
		kb := b.kids[i]
		if ka.key != kb.key || ka.n != kb.n || ka.avail != kb.avail {
			return fmt.Sprintf("%s/%s: edge n %d/%d avail %d/%d", path, ka.key, ka.n, kb.n, ka.avail, kb.avail)
		}
		if math.Abs(ka.w-(float64(kb.n)-kb.w)) > tol*float64(max(ka.n, 1)) {
			return fmt.Sprintf("%s/%s: edge w %v vs mirrored %v", path, ka.key, ka.w, float64(kb.n)-kb.w)
		}
		if (ka.next == nil) != (kb.next == nil) {
			return fmt.Sprintf("%s/%s: expanded in one tree only", path, ka.key)
		}
		if ka.next != nil {
			if d := sameTreeMirrored(ka.next, kb.next, path+"/"+string(ka.key), tol); d != "" {
				return d
			}
		}
	}
	return ""
}

// A tree and its mirror -- the same game from the other seat, root owner
// included -- spend their visits identically, and every value of one is 1
// minus the other's: the root's value, every node's and every edge's. Over
// both first-play urgency and an absolute unvisited Q, and with the action
// discount (which commutes with 1 - v).
func TestOpponentNodeSymmetry(t *testing.T) {
	type knob struct {
		name string
		set  func(o *Options)
	}
	knobs := []knob{
		{"fpu", func(o *Options) {}},
		{"absolute-q-discount", func(o *Options) {
			o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = 0.5, true, 0.3
			o.Discount, o.DiscountUnit = 0.9, DiscountAction
		}},
	}
	for seed := uint64(1); seed <= 12; seed++ {
		g := randomOppGame(rand.New(rand.NewPCG(seed, seed^0x51)), 6)
		m := mirrorGame(g)
		for _, k := range knobs {
			opts := treeOpts(300)
			k.set(&opts)
			var sa, sb Stats
			ta, err := runTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &sa)
			if err != nil {
				t.Fatal(err)
			}
			tb, err := runTree(context.Background(), rootOf(m), &fakeSource{g: m}, opts, &sb)
			if err != nil {
				t.Fatal(err)
			}
			if d := sameTreeMirrored(ta, tb, "", 1e-9); d != "" {
				t.Fatalf("seed %d %s: %s", seed, k.name, d)
			}
			ra, rb := treeResult(rootOf(g), ta), treeResult(rootOf(m), tb)
			if math.Abs(ra.RootValue-(1-rb.RootValue)) > 1e-9 {
				t.Fatalf("seed %d %s: root values %v and %v do not sum to 1", seed, k.name, ra.RootValue, rb.RootValue)
			}
			if sa.OppPoints == 0 || sa.OppPoints+sb.OppPoints != sa.Plays {
				t.Fatalf("seed %d %s: opponent selections %d + mirrored %d, want %d plays split between them", seed, k.name, sa.OppPoints, sb.OppPoints, sa.Plays)
			}
		}
	}
}

// --- 9. An owner flip discards the simulation ------------------------------

// From the second world on, the decision at /a belongs to the other seat
// than the tree's node there: every later simulation through it is
// discarded as a submit error, and the tree keeps only the first.
func TestOpponentNodeOwnerFlipIsASubmitError(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":     {keys: []Key{"a"}, prior: []float64{1}, value: 0.5},
			"/a":   {keys: []Key{"x", "y"}, prior: []float64{0.5, 0.5}, value: 0.4, opp: true},
			"/a/x": {value: 0.9, terminal: true},
			"/a/y": {value: 0.1, terminal: true},
		},
		oppFlip: map[string]bool{"/a": true},
	}
	var st Stats
	top, err := runTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(4), &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.Completed != 1 || st.SubmitErrors != 3 || top.n != 2 {
		t.Fatalf("stats %+v root n %d, want 1 completed and 3 submit errors", st, top.n)
	}
	if !errors.Is(errOwner, ErrSubmit) {
		t.Fatal("errOwner is not an ErrSubmit")
	}
	// The root itself offered for the other seat.
	g2 := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a"}, prior: []float64{1}, value: 0.5, opp: true},
		"/a": {value: 1, terminal: true},
	}}
	actorRoot := &Point{Keys: []Key{"a"}, Prior: []float64{1}}
	var st2 Stats
	if _, err := RunTree(context.Background(), actorRoot, &fakeSource{g: g2}, treeOpts(2), &st2); err != nil {
		t.Fatal(err)
	}
	if st2.Completed != 0 || st2.SubmitErrors != 2 {
		t.Fatalf("a root offered to the other seat: stats %+v", st2)
	}
	// The node cache's fixed-world check compares the owner too.
	nd := &node{pt: &Point{Keys: []Key{"x"}, Prior: []float64{1}, opp: true}}
	if samePoint(&Point{Keys: []Key{"x"}, Prior: []float64{1}}, nd) {
		t.Fatal("samePoint ignores the owner")
	}
}

// --- Engine tests ------------------------------------------------------------

// oppOpts are the knob sets the engine tests run with opponent nodes on: the
// spec's defaults, and the search benchmark's / mzplay's (auto-pay
// candidates with the opponent's own auto-pay bot, name keys, the absolute
// unvisited Q, the action discount, no candidate cap).
func oppOpts(bench bool, sims int, seed uint64) Options {
	o := DefaultOptions()
	o.Sims, o.Seed = sims, seed
	if bench {
		o.CPUCT, o.AbsoluteUnvisitedQ, o.UnvisitedQ = 0.5, true, 0.5
		o.Limit, o.AutoPayment, o.UniformPrior, o.NameKeys = BenchCandidateLimit, true, true, true
		o.Discount, o.DiscountUnit = 0.99, DiscountAction
	}
	o.OpponentNodes = true
	return o
}

func oppSearch(t *testing.T, r measureRoot, kind string, opts Options, macros bool) Result {
	t.Helper()
	obs := searchprobe.NewCollector(r.d.Player)
	root := Root{Engine: r.e, Decision: r.d, Bot: r.bot, Observer: obs}
	if macros {
		root.Macros, root.BotKey = landMacros(r.d, r.bot)
	}
	res, err := Search(context.Background(), root, measureSource(kind, r.e, obs), nil, opts)
	if err != nil {
		t.Fatalf("%s %s: %v", r.name, kind, err)
	}
	return res
}

// --- 6. Determinism on, and 7. the node cache stays exact ------------------

// With opponent nodes on, the same seed gives a byte-identical Result
// (test 6), and a search with the node cache (every cap, eviction included)
// returns the same Result as one without it but for the walk's cost
// counters (test 7) -- over both fixed-world sources and both knob sets.
// The trees really hold opponent nodes.
func TestOpponentNodesDeterministicAndCacheExact(t *testing.T) {
	roots := measureRoots(t, 8)
	sims := 120
	if testing.Short() {
		roots, sims = roots[:3], 50
	}
	var oppPoints, oppExpanded, resumes int
	for i, r := range roots {
		for _, kind := range []string{"clairvoyant", "pimc"} {
			for _, bench := range []bool{false, true} {
				run := func(cache int) Result {
					o := oppOpts(bench, sims, uint64(i)*17+9)
					o.NodeCache = cache
					o.Noise = !bench && i%2 == 1
					return oppSearch(t, r, kind, o, bench)
				}
				off := run(0)
				if again := run(0); !reflect.DeepEqual(off, again) {
					t.Fatalf("%s %s bench %v: two searches with one seed differ", r.name, kind, bench)
				}
				oppPoints += off.Stats.OppPoints
				oppExpanded += off.Stats.OppExpanded
				if off.Stats.OppPoints > 0 && off.Stats.OppExpanded == 0 {
					t.Fatalf("%s %s: opponent selections without an opponent node", r.name, kind)
				}
				for _, cache := range []int{1, 3, 17, DefaultNodeCache} {
					on := run(cache)
					if d := sameResult(off, on); d != "" {
						t.Fatalf("%s %s bench %v cache %d: the cached search differs: %s", r.name, kind, bench, cache, d)
					}
					if cache == DefaultNodeCache {
						if again := run(cache); !reflect.DeepEqual(on, again) {
							t.Fatalf("%s %s bench %v: two cached searches with one seed differ", r.name, kind, bench)
						}
					}
					resumes += on.Stats.NodeResumes
				}
			}
		}
	}
	if oppPoints == 0 || oppExpanded == 0 || resumes == 0 {
		t.Fatalf("never exercised: %d opponent selections, %d opponent nodes, %d resumes", oppPoints, oppExpanded, resumes)
	}
	t.Logf("%d roots x 2 sources x 2 knob sets: deterministic and cache-exact; %d opponent selections, %d opponent nodes, %d resumes",
		len(roots), oppPoints, oppExpanded, resumes)
}

// --- 3. Symmetry on the engine ---------------------------------------------

// hasFaceDown reports whether any object of e is face down: the heuristic
// leaf's redaction then depends on the viewer, and is not antisymmetric.
func hasFaceDown(e *rules.Engine) bool {
	for i := range e.G.Players {
		for _, z := range []state.Zone{state.ZBattlefield, state.ZExile, state.ZStack, state.ZHand, state.ZGraveyard} {
			for _, id := range e.G.Zone(z, e.G.Players[i].ID) {
				if o := e.G.Obj(id); o != nil && o.FaceDown {
					return true
				}
			}
		}
	}
	return false
}

// The same engine search valued from the other seat (the test-only
// swapFrame: the root and the searching seat's points are opponent nodes,
// the opponent's are not, every leaf is the opponent's heuristic value)
// spends its visits identically and its values are 1 minus the original's:
// the root value and every root Q, within 1e-6 (the heuristic leaf is
// antisymmetric only to rounding).
func TestOpponentNodesEngineSymmetry(t *testing.T) {
	roots := measureRoots(t, 8)
	sims := 100
	if testing.Short() {
		roots, sims = roots[:3], 40
	}
	checked := 0
	for i, r := range roots {
		if hasFaceDown(r.e) {
			continue
		}
		for _, bench := range []bool{false, true} {
			o := oppOpts(bench, sims, uint64(i)*5+2)
			a := oppSearch(t, r, "clairvoyant", o, bench)
			o.swapFrame = true
			b := oppSearch(t, r, "clairvoyant", o, bench)
			if a.Stats.Completed == 0 || a.Stats.OppPoints == 0 {
				continue
			}
			if !reflect.DeepEqual(a.Visits, b.Visits) || !reflect.DeepEqual(a.Avail, b.Avail) {
				t.Fatalf("%s bench %v: visits %v / %v avail %v / %v", r.name, bench, a.Visits, b.Visits, a.Avail, b.Avail)
			}
			if math.Abs(a.RootValue-(1-b.RootValue)) > 1e-6 {
				t.Fatalf("%s bench %v: root values %v and %v", r.name, bench, a.RootValue, b.RootValue)
			}
			for k := range a.Q {
				if a.Visits[k] > 0 && math.Abs(a.Q[k]-(1-b.Q[k])) > 1e-6 {
					t.Fatalf("%s bench %v: Q[%d] %v and %v", r.name, bench, k, a.Q[k], b.Q[k])
				}
			}
			if a.Choice != b.Choice || a.Stats.Expanded != b.Stats.Expanded || b.Stats.OppExpanded != a.Stats.Expanded-a.Stats.OppExpanded {
				t.Fatalf("%s bench %v: choice %d/%d stats %+v / %+v", r.name, bench, a.Choice, b.Choice, a.Stats, b.Stats)
			}
			checked++
		}
	}
	if checked < 4 {
		t.Fatalf("only %d searches checked", checked)
	}
	t.Logf("%d searches symmetric", checked)
}

// --- 8. Refusals ---------------------------------------------------------------

func TestOpponentNodesRefusals(t *testing.T) {
	for _, o := range []Options{
		func() Options { o := oppOpts(false, 8, 1); o.RootPerWorld = true; return o }(),
		func() Options { o := oppOpts(false, 8, 1); o.OpponentLimit = 1; return o }(),
		func() Options { o := oppOpts(false, 8, 1); o.OpponentLimit = -1; return o }(),
		func() Options { o := DefaultOptions(); o.swapFrame = true; return o }(),
	} {
		if o.Validate(nil) == nil {
			t.Errorf("options %+v accepted", o)
		}
	}
	if err := oppOpts(true, 8, 1).Validate(nil); err != nil {
		t.Errorf("opponent nodes refused: %v", err)
	}
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, "", 0, 2000)
	// A world source whose worlds differ between simulations.
	obs := searchprobe.NewCollector(d.Player)
	if _, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, hypSource{e: e, obs: obs}, nil, oppOpts(false, 8, 1)); err == nil || !strings.Contains(err.Error(), "fixed world") {
		t.Errorf("a re-seeded source: err %v", err)
	}
	// The same search with the switch off runs.
	if _, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, hypSource{e: e, obs: obs}, nil, func() Options { o := oppOpts(false, 8, 1); o.OpponentNodes = false; return o }()); err != nil {
		t.Errorf("switch off: %v", err)
	}
	// The redeal seat.
	sc := DefaultSeatConfig()
	sc.World, sc.Search.OpponentNodes = WorldRedeal, true
	if _, err := NewSeat(1, nil, sc); err == nil {
		t.Error("a redeal seat with opponent nodes accepted")
	}
	sc.World, sc.Source = WorldClairvoyant, testSeatSource
	if _, err := NewSeat(1, nil, sc); err != nil {
		t.Errorf("a clairvoyant seat with opponent nodes refused: %v", err)
	}
	// A three-player game.
	reg := testutil.CorpusRegistry(t)
	var decks [][]*cards.Card
	for _, n := range []string{"mono-red-prowess", "mono-blue-tempo", "uw-tempo"} {
		dk, err := testutil.LoadRepoDeck(reg, n)
		if err != nil {
			t.Fatal(err)
		}
		decks = append(decks, dk)
	}
	e3 := rules.New(rules.Config{Seed: testSeed, Names: []string{"a", "b", "c"}, Decks: decks, Tokens: reg.Tokens})
	e3.Advance()
	d3 := e3.Pending()
	if d3 == nil {
		t.Fatal("no pending decision in the three-player game")
	}
	obs3 := searchprobe.NewCollector(d3.Player)
	src3, _ := newTestClairvoyant(e3, obs3)
	if _, err := Search(context.Background(), Root{Engine: e3, Decision: d3, Observer: obs3}, src3, nil, oppOpts(false, 8, 1)); err == nil || !strings.Contains(err.Error(), "two-player") {
		t.Errorf("a three-player game: err %v", err)
	}
}

// --- 10. The opponent's candidate cap ----------------------------------------

// OpponentLimit caps the candidates at an opponent node and every cut is
// counted in Stats.Truncated: with no cap on the actor (the benchmark's
// limit) and the opponent capped at 2, every Truncated is an opponent
// node's, no opponent node holds more than 2 children, and the uncapped
// run truncates nothing.
func TestOpponentLimitIsCountedInTruncated(t *testing.T) {
	roots := measureRoots(t, 8)
	var maxKids int
	var cutTotal int
	defer func() { searchTreeHook = nil }()
	for i, r := range roots {
		o := oppOpts(true, 80, uint64(i)+41)
		o.NodeCache = 0
		uncapped := oppSearch(t, r, "clairvoyant", o, true)
		if uncapped.Stats.Truncated != 0 {
			t.Fatalf("%s: the uncapped search truncated %d points", r.name, uncapped.Stats.Truncated)
		}
		o.OpponentLimit = 2
		searchTreeHook = func(top *node) {
			var walk func(n *node)
			walk = func(n *node) {
				if n.opp && len(n.kids) > maxKids {
					maxKids = len(n.kids)
				}
				for _, k := range n.kids {
					if k.next != nil {
						walk(k.next)
					}
				}
			}
			walk(top)
		}
		capped := oppSearch(t, r, "clairvoyant", o, true)
		searchTreeHook = nil
		cutTotal += capped.Stats.Truncated
		o.NodeCache = DefaultNodeCache
		if cached := oppSearch(t, r, "clairvoyant", o, true); cached.Stats.Truncated != capped.Stats.Truncated {
			t.Fatalf("%s: Truncated %d with the cache, %d without", r.name, cached.Stats.Truncated, capped.Stats.Truncated)
		}
	}
	if cutTotal == 0 {
		t.Fatal("the opponent cap never cut a point")
	}
	if maxKids > 2 {
		t.Fatalf("an opponent node holds %d children under OpponentLimit 2", maxKids)
	}
	t.Logf("%d opponent points cut over %d roots", cutTotal, len(roots))
}

// --- 4. An obvious reply the bot declines ----------------------------------

// combatLeaf is a leaf that makes the opponent's block an obvious reply:
// over the events since the root, up to the next turn, an attack the
// opponent blocked is worth 0.1 to the searching seat, an unblocked one 0.9,
// and no attack 0.5.
func combatLeaf(n0 int) LeafFunc {
	return func(w *rules.Engine, actor state.PlayerID) float64 {
		attacked, blocked := false, false
		for _, ev := range w.L.Events[n0:] {
			switch {
			case ev.Kind == events.TurnChange:
				goto done
			case ev.Kind == events.DeclareAttackers:
				attacked = true
			case ev.Kind == events.DeclareBlockers && len(ev.Pairs) > 0:
				blocked = true
			}
		}
	done:
		switch {
		case !attacked:
			return 0.5
		case blocked:
			return 0.1
		}
		return 0.9
	}
}

// At three attack roots the opponent can block, but its bot declines to: an
// attack looks worth 0.9 without opponent nodes and the searching seat
// attacks. With opponent nodes the opponent's block is searched, valued
// from its side (0.9 to it), so the attack falls toward 0.1 and the
// searching seat holds back. The tree shows why: below the attack, an
// opponent node whose bot answer (candidate 0) leaves the attack unblocked
// while its most-visited reply blocks.
func TestOpponentNodesFindTheObviousReply(t *testing.T) {
	cases := []struct {
		a, b string
		seed uint64
		turn int32
	}{
		{"uw-tempo", "mono-blue-tempo", 90000006, 3},
		{"mono-red-prowess", "mono-blue-tempo", 90000006, 7},
		{"mono-green-stompy", "ur-delver", 90000008, 5},
	}
	defer func() { searchTreeHook = nil }()
	for _, c := range cases {
		cfg := testConfig(t, c.a, c.b, c.seed)
		e, d, bot := botPosition(t, cfg, decision.KAttackers, c.turn, 6000)
		r := measureRoot{name: fmt.Sprintf("%s/%d/t%d", c.a, c.seed, c.turn), e: e, d: d, bot: bot}
		o := oppOpts(true, 200, c.seed%97+11)
		o.Leaf = combatLeaf(len(e.L.Events))
		o.OpponentNodes = false
		off := oppSearch(t, r, "clairvoyant", o, true)
		o.OpponentNodes = true
		var top *node
		searchTreeHook = func(n *node) { top = n }
		on := oppSearch(t, r, "clairvoyant", o, true)
		searchTreeHook = nil
		if off.Labels[off.Choice] == "no attack" || on.Labels[on.Choice] != "no attack" {
			t.Fatalf("%s: off plays %q (Q %v), on %q (Q %v); want an attack off and none on", r.name, off.Labels[off.Choice], off.Q, on.Labels[on.Choice], on.Q)
		}
		// The opponent's declined reply, below the attack off chose.
		found := false
		var walk func(n *node)
		walk = func(n *node) {
			if n == nil || found {
				return
			}
			if n.opp && len(n.kids) > 1 && n.kids[0].n > 0 {
				best := 0
				for j, k := range n.kids {
					if k.n > n.kids[best].n {
						best = j
					}
				}
				q := func(k *edge) float64 { return k.w / float64(k.n) }
				if best != 0 && q(n.kids[0]) > 0.8 && q(n.kids[best]) < 0.2 {
					found = true
					return
				}
			}
			for _, k := range n.kids {
				walk(k.next)
			}
		}
		walk(top.child(off.Keys[off.Choice]).next)
		if !found {
			t.Fatalf("%s: no opponent node below the attack whose most-visited reply blocks against its bot's answer", r.name)
		}
		t.Logf("%s: off %q Q %.2f; on %q (the attack's Q %.2f)", r.name, off.Labels[off.Choice], off.Q[off.Choice], on.Labels[on.Choice], on.Q[off.Choice])
	}
}
