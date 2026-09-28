package sbsearch

import (
	"math"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	gbench "github.com/adams-shaun/gorge/internal/bench"
	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// testGame is one SpellBench-style mirror: the Rally deck in both seats,
// seat 0 built by mk, seat 1 plain sb-tactical, seeded the way
// cmd/botbench's sbPlay seeds a pair (seat seed = game seed ^ seat+1).
type testGame struct {
	reg    *cards.Registry
	deck   []*cards.Card
	lookup builtins.CardLookup
}

func newTestGame(t *testing.T) *testGame {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cat, err := spellbench.CatalogByID("pauper-kernel")
	if err != nil {
		t.Fatal(err)
	}
	deck, err := spellbench.Deck(reg, cat.Dir, "Rally")
	if err != nil {
		t.Fatal(err)
	}
	return &testGame{reg: reg, deck: deck, lookup: builtins.NewRegistryLookup(reg)}
}

func (g *testGame) tactical(seed uint64) *builtins.Seat {
	return builtins.NewTactical(builtins.AutoPay, seed, g.lookup, builtins.DefaultTacticalWeights())
}

// play returns the finished game's chain head and outcome.
func (g *testGame) play(t *testing.T, seed uint64, mk func(seed uint64) seat.Seat) (string, gbench.Outcome) {
	t.Helper()
	seats := []seat.Seat{mk(seed ^ 1), g.tactical(seed ^ 2)}
	cfg := rules.Config{Seed: seed, Names: []string{"p0", "p1"}, Decks: [][]*cards.Card{g.deck, g.deck}, Tokens: g.reg.Tokens, NameUniverse: g.reg.Cards}
	hooks := gbench.Hooks{Setup: func(e *rules.Engine) {
		for _, s := range seats {
			if u, ok := s.(interface{ UnwrapSeat() seat.Seat }); ok {
				s = u.UnwrapSeat()
			}
			if b, ok := s.(*builtins.Seat); ok {
				b.SetPlanner(e)
			}
		}
	}}
	o, e, err := gbench.PlayGame(cfg, seats, 60, 20000, hooks)
	if err != nil {
		t.Fatal(err)
	}
	if o.IsStalled() {
		t.Fatalf("game stalled: %+v", o)
	}
	return e.L.Head(), o
}

// quick is a small search budget: the tests are about the contract, not
// strength.
func quick() Config {
	return Config{Worlds: 2, TopK: 2, Horizon: 1, MaxSteps: 400, Margin: 0}
}

// watch collects the Diags of one test (tests in this package run
// sequentially; Watch is package state).
func watch(t *testing.T) *[]Diag {
	var mu sync.Mutex
	var ds []Diag
	old := Watch
	Watch = func(d Diag) { mu.Lock(); ds = append(ds, d); mu.Unlock() }
	t.Cleanup(func() { Watch = old })
	return &ds
}

const testSeed = 0x5b5ea4c7

// TestZeroWorldsIsTactical: with W=0 sb-search is sb-tactical, decision for
// decision -- the whole game's event chain is identical.
func TestZeroWorldsIsTactical(t *testing.T) {
	g := newTestGame(t)
	for _, seed := range []uint64{testSeed, testSeed + 1} {
		want, _ := g.play(t, seed, func(s uint64) seat.Seat { return g.tactical(s) })
		got, _ := g.play(t, seed, func(s uint64) seat.Seat {
			c := quick()
			c.Worlds = 0
			return New(g.tactical(s), s, c)
		})
		if got != want {
			t.Fatalf("seed %d: sb-search W=0 head %s != sb-tactical %s", seed, got, want)
		}
	}
}

// TestForcedErrorFallsBackToTactical: when every rollout fails, every
// searched decision plays sb-tactical's own pick, so the game is exactly
// sb-tactical's -- and the searches did run and fail.
func TestForcedErrorFallsBackToTactical(t *testing.T) {
	g := newTestGame(t)
	want, _ := g.play(t, testSeed, func(s uint64) seat.Seat { return g.tactical(s) })
	ds := watch(t)
	got, _ := g.play(t, testSeed, func(s uint64) seat.Seat {
		st := New(g.tactical(s), s, quick())
		st.failRollout = true
		return st
	})
	if got != want {
		t.Fatalf("failing sb-search head %s != sb-tactical %s", got, want)
	}
	if len(*ds) == 0 {
		t.Fatal("no decision was searched")
	}
	for _, d := range *ds {
		if d.Override || d.Worlds != 0 || d.Failed == 0 {
			t.Fatalf("a searched decision with every rollout failing: %+v", d)
		}
	}
}

// TestSearchIsDeterministic: the same seed plays the same game, searches
// included, and the search does change play somewhere (an override).
func TestSearchIsDeterministic(t *testing.T) {
	g := newTestGame(t)
	ds := watch(t)
	mk := func(s uint64) seat.Seat { return New(g.tactical(s), s, quick()) }
	a, oa := g.play(t, testSeed, mk)
	n := len(*ds)
	b, ob := g.play(t, testSeed, mk)
	if a != b || oa != ob {
		t.Fatalf("two runs differ: %s %+v vs %s %+v", a, oa, b, ob)
	}
	if n == 0 || len(*ds) != 2*n {
		t.Fatalf("searched decisions: first run %d, both %d", n, len(*ds))
	}
	valid, over := 0, 0
	for _, d := range (*ds)[:n] {
		valid += d.Worlds
		if d.Override {
			over++
		}
		if d.Refused != "" {
			t.Logf("failure: %s", d.Refused)
		}
	}
	if valid == 0 {
		t.Fatal("no world was ever completed")
	}
	t.Logf("%d searched decisions, %d valid worlds, %d overrides", n, valid, over)
}

// TestPickRoots: sb-tactical's pick first, then by score, pass appended.
func TestPickRoots(t *testing.T) {
	c := func(kind string, sc float64) builtins.ScoredCandidate {
		return builtins.ScoredCandidate{Key: builtins.PriorityKey{Kind: kind, Opt: -1}, Score: sc}
	}
	cands := []builtins.ScoredCandidate{c("pass", 0), c("cast", 5), c("ability", 9), c("cast2", 7)}
	cands[0].Key.Opt = 0
	roots, gap := pickRoots(cands, 2, 2)
	if gap != 2 || len(roots) != 3 || roots[0].key.Kind != "ability" || roots[1].key.Kind != "cast2" || !roots[2].key.IsPass() {
		t.Fatalf("roots %+v %+v %+v", roots[0].key, roots[1].key, roots[2].key)
	}
}

// TestCombatAndTargetSearch: blockers and single-target decisions are
// searched (Block, Target), deterministically, and every searched kind
// completes worlds.
func TestCombatAndTargetSearch(t *testing.T) {
	g := newTestGame(t)
	ds := watch(t)
	cfg := quick()
	cfg.Attack, cfg.Block, cfg.Target = true, true, true
	mk := func(s uint64) seat.Seat { return New(g.tactical(s), s, cfg) }
	kinds := map[string]int{}
	valid := map[string]int{}
	for _, seed := range []uint64{testSeed, testSeed + 1, testSeed + 2} {
		*ds = nil
		a, oa := g.play(t, seed, mk)
		n := len(*ds)
		for _, d := range (*ds)[:n] {
			kinds[d.Kind]++
			valid[d.Kind] += d.Worlds
		}
		b, ob := g.play(t, seed, mk)
		if a != b || oa != ob || len(*ds) != 2*n {
			t.Fatalf("seed %d: two runs differ: %s %+v vs %s %+v (%d/%d searched)", seed, a, oa, b, ob, n, len(*ds)-n)
		}
	}
	t.Logf("searched %v, valid worlds %v", kinds, valid)
	if kinds["blockers"] == 0 || valid["blockers"] == 0 {
		t.Fatalf("no blockers decision searched: %v %v", kinds, valid)
	}
}

// TestAdaptiveWorlds: a decision stops early only after MinWorlds valid
// worlds and extends past Worlds only up to MaxWorlds.
func TestAdaptiveWorlds(t *testing.T) {
	g := newTestGame(t)
	ds := watch(t)
	cfg := quick()
	cfg.Worlds, cfg.MinWorlds, cfg.StopBelow, cfg.MaxWorlds, cfg.CloseBand = 3, 1, 0, 5, 1
	g.play(t, testSeed, func(s uint64) seat.Seat { return New(g.tactical(s), s, cfg) })
	short, long := 0, 0
	for _, d := range *ds {
		if d.Worlds+d.Failed > 5 || (d.Worlds < 1 && d.Failed == 0) {
			t.Fatalf("world count out of bounds: %+v", d)
		}
		if d.Worlds < 3 && d.Failed == 0 {
			short++
		}
		if d.Worlds > 3 {
			long++
		}
	}
	t.Logf("%d decisions, %d stopped early, %d extended", len(*ds), short, long)
	if long == 0 {
		t.Fatal("CloseBand 1 extends every decision whose lead is finite, yet none was")
	}
}

// TestFittedLeafSymmetric: the fitted leaf is a zero-sum win probability,
// the two seats' values of one view sum to 1, and more life is better.
func TestFittedLeafSymmetric(t *testing.T) {
	mk := func(l0, l1 int32) view.View {
		return view.View{Active: 0, Players: []view.PlayerView{
			{ID: 0, Life: l0, HandSize: 3, LibrarySize: 30, Battlefield: []view.CardView{{Types: "Land"}, {Types: "Creature", Power: 2, Toughness: 2}}},
			{ID: 1, Life: l1, HandSize: 5, LibrarySize: 2, Battlefield: []view.CardView{{Types: "Creature", Power: 3, Toughness: 1, Keywords: []string{"Flying"}}}},
		}}
	}
	v := mk(12, 7)
	a, b := fittedLeaf(v, 0), fittedLeaf(v, 1)
	if math.Abs(a+b-1) > 1e-12 || a <= 0 || a >= 1 {
		t.Fatalf("leaf %v + %v != 1", a, b)
	}
	if fittedLeaf(mk(13, 7), 0) <= a || fittedLeaf(mk(12, 6), 0) <= a {
		t.Fatal("more life (or less opposing life) did not raise the leaf")
	}
	if d := fittedLeaf(mk(400, 7), 0) - fittedLeaf(mk(300, 7), 0); d < 0 || d > 1e-3 {
		t.Fatalf("life above the cap moved the leaf by %v (only the opposing clock term reads it)", d)
	}
}
