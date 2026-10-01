package azmcts

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// clockEnv is fakeEnv on an engine clock (PathEnv): clocks maps a path to
// its (plies, turn).
type clockEnv struct {
	fakeEnv
	clocks map[string][2]int
}

func (c *clockEnv) Plies() int { return c.clocks[c.path][0] }
func (c *clockEnv) Turn() int  { return c.clocks[c.path][1] }

type clockSource struct {
	g      *fakeGame
	clocks map[string][2]int
}

func (s *clockSource) Env(sim int) (Env, error) {
	return &clockEnv{fakeEnv: fakeEnv{g: s.g, sim: sim}, clocks: s.clocks}, nil
}

// Two simulations on root -a-> /a -x-> /a/x (terminal, 1). Sim 0 evaluates
// the root (0.5) and expands /a (0.8); sim 1 reaches /a/x. The clock: root
// at ply 0 turn 1, /a at ply 3 turn 2 (the searched a plus two environment
// answers, across the turn), /a/x at ply 4 turn 2.
//
// Under each unit, sim 1's Delta is: root node 4 plies / 2 edges / 1 turn;
// edge a (the position /a) 1 / 1 / 0. Sim 0's: root node 3 / 1 / 1; edge a
// 0 (the new leaf keeps its value).
func TestDiscountPerUnit(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":     {keys: []Key{"a"}, prior: []float64{1}, value: 0.5},
		"/a":   {keys: []Key{"x"}, prior: []float64{1}, value: 0.8},
		"/a/x": {value: 1, terminal: true},
	}}
	clocks := map[string][2]int{"": {0, 1}, "/a": {3, 2}, "/a/x": {4, 2}}
	const gamma = 0.9
	disc := func(v float64, d int) float64 { return 0.5 + (v-0.5)*math.Pow(gamma, float64(d)) }
	for _, tc := range []struct {
		unit             DiscountUnit
		edgeA            float64 // edge a's Q
		root             float64 // the root's mean, its own evaluation included
		plies, edges, tr int
	}{
		{DiscountPly, (0.8 + disc(1, 1)) / 2, (0.5 + disc(0.8, 3) + disc(1, 4)) / 3, 7, 3, 2},
		{DiscountAction, (0.8 + disc(1, 1)) / 2, (0.5 + disc(0.8, 1) + disc(1, 2)) / 3, 7, 3, 2},
		{DiscountTurn, (0.8 + 1) / 2, (0.5 + disc(0.8, 1) + disc(1, 1)) / 3, 7, 3, 2},
	} {
		opts := treeOpts(2)
		opts.Discount, opts.DiscountUnit = gamma, tc.unit
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &clockSource{g: g, clocks: clocks}, opts, &st)
		if err != nil {
			t.Fatal(err)
		}
		if !near(tr.Q[0], tc.edgeA) || !near(tr.RootValue, tc.root) {
			t.Errorf("%s: edge Q %v root %v, want %v %v", tc.unit, tr.Q[0], tr.RootValue, tc.edgeA, tc.root)
		}
		if st.LeafPlies != tc.plies || st.LeafEdges != tc.edges || st.LeafTurns != tc.tr {
			t.Errorf("%s: depth sums plies %d edges %d turns %d", tc.unit, st.LeafPlies, st.LeafEdges, st.LeafTurns)
		}
		if !near(st.MeanLeafPlies(), 3.5) || !near(st.MeanLeafEdges(), 1.5) || !near(st.MeanLeafTurns(), 1) {
			t.Errorf("%s: means %v %v %v", tc.unit, st.MeanLeafPlies(), st.MeanLeafEdges(), st.MeanLeafTurns())
		}
	}
	// Off (the default): the plain means.
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &clockSource{g: g, clocks: clocks}, treeOpts(2), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !near(tr.Q[0], 0.9) || !near(tr.RootValue, (0.5+0.8+1)/3) {
		t.Errorf("undiscounted: edge Q %v root %v", tr.Q[0], tr.RootValue)
	}
}

func TestDiscountValueShrinksTowardUnknown(t *testing.T) {
	for _, tc := range []struct {
		v, g float64
		d    int
		want float64
	}{
		{1, 0.9, 1, 0.95}, {0, 0.9, 1, 0.05}, {0.5, 0.5, 7, 0.5}, {0.8, 0.9, 0, 0.8}, {1, 0.5, 2, 0.625},
	} {
		if got := discountValue(tc.v, tc.g, tc.d); !near(got, tc.want) {
			t.Errorf("discountValue(%v, %v, %d) = %v, want %v", tc.v, tc.g, tc.d, got, tc.want)
		}
	}
}

// An absolute unvisited Q replaces first-play urgency. Sim 0 evaluates the
// root (0.9) and plays a (the tie-winner), which scores 0.2. At sim 1,
// with c = 0.1 and equal priors: U(a) = 0.1*0.5*sqrt(2)/2, U(b) =
// 0.1*0.5*sqrt(2). Under FPU b reads 0.55-0.1 = 0.45 and is tried; with
// UnvisitedQ 0.1 it reads 0.1 < a's 0.2 + U(a), so a is played again.
func TestAbsoluteUnvisitedQ(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.9},
		"/a": {value: 0.2, terminal: true},
		"/b": {value: 0.6, terminal: true},
	}}
	for _, tc := range []struct {
		abs  bool
		want []int
	}{{false, []int{1, 1}}, {true, []int{2, 0}}} {
		opts := treeOpts(2)
		opts.CPUCT = 0.1
		opts.AbsoluteUnvisitedQ, opts.UnvisitedQ = tc.abs, 0.1
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, opts, &st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tr.Visits, tc.want) {
			t.Errorf("absolute %v: visits %v, want %v", tc.abs, tr.Visits, tc.want)
		}
	}
	bad := DefaultOptions()
	bad.AbsoluteUnvisitedQ, bad.UnvisitedQ = true, 1.5
	if bad.Validate(nil) == nil {
		t.Error("an unvisited Q outside [0,1] validated")
	}
	bad = DefaultOptions()
	bad.Discount = 1.2
	if bad.Validate(nil) == nil {
		t.Error("a discount above 1 validated")
	}
}

func TestParseDiscountUnit(t *testing.T) {
	for i, n := range DiscountUnitNames {
		u, err := ParseDiscountUnit(n)
		if err != nil || int(u) != i || u.String() != n {
			t.Errorf("%s: %v %v", n, u, err)
		}
	}
	if _, err := ParseDiscountUnit("step"); err == nil {
		t.Error("unknown unit parsed")
	}
}

// The candidate limit's cut is counted, and the benchmark limit cuts
// nothing: every candidate a smaller limit lists is a prefix of the
// benchmark's list.
func TestCandidateTruncationIsCounted(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 4, 6000)
	full, _, _, ok, cut := enumerateCut(searchprobe.NewCollector(0), e, d, bot, AllKinds(), BenchCandidateLimit, false)
	if !ok || cut {
		t.Fatalf("benchmark limit: ok %v cut %v", ok, cut)
	}
	if len(full) < 3 {
		t.Skipf("position has only %d candidates", len(full))
	}
	opts := DefaultOptions()
	opts.Sims, opts.Seed, opts.Limit = 6, 3, 2
	res := searchAt(t, e, d, bot, nil, opts)
	if res.Stats.RootTruncated != 1 || res.Stats.Truncated < 1 || len(res.Keys) != 2 {
		t.Fatalf("limit 2 over %d candidates: root truncated %d, truncated %d, %d keys", len(full), res.Stats.RootTruncated, res.Stats.Truncated, len(res.Keys))
	}
	for i, k := range res.Keys {
		if full[i].key != k {
			t.Fatalf("candidate %d differs under the smaller limit", i)
		}
	}
	opts.Limit = BenchCandidateLimit
	res = searchAt(t, e, d, bot, nil, opts)
	if res.Stats.RootTruncated != 0 || len(res.Keys) != len(full) {
		t.Fatalf("benchmark limit: root truncated %d, %d keys of %d", res.Stats.RootTruncated, len(res.Keys), len(full))
	}
}

// Every root key maps back to its own intent through IntentForKey, on the
// engine and on a clone (with a fresh observer): keys name actions across
// engines. The auto-pay vocabulary keeps pass and each payable cast once.
func TestIntentForKeyRoundTrips(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	for _, auto := range []bool{false, true} {
		e, d, bot := botPosition(t, cfg, decision.KPriority, 4, 6000)
		if auto {
			// The auto-pay vocabulary is asked with the auto-pay bot's answer
			// (a planned cast or pass), as the benchmark arms ask it.
			e.EnsurePaymentActions()
			bot, _ = seat.NewBot(7).EnableAutoPayMana().Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
		}
		obs := searchprobe.NewCollector(0)
		if _, err := obs.Capture(e, nil); err != nil {
			t.Fatal(err)
		}
		cands, _, _, ok, _ := enumerateCut(obs, e, d, bot, AllKinds(), BenchCandidateLimit, auto)
		if !ok {
			t.Fatalf("auto %v: no candidates for bot answer %+v", auto, bot)
		}
		seen := map[Key]bool{}
		clone := e.Clone()
		cobs := searchprobe.NewCollector(0)
		if _, err := cobs.Capture(clone, nil); err != nil {
			t.Fatal(err)
		}
		for _, c := range cands {
			if seen[c.key] {
				t.Fatalf("auto %v: duplicate key %s", auto, c.key)
			}
			seen[c.key] = true
			got, err := IntentForKey(obs, e, d, c.key)
			if err != nil || !sameIntent(got, c.in) {
				t.Fatalf("auto %v: key %s: %+v %v, want %+v", auto, c.key, got, err, c.in)
			}
			got, err = IntentForKey(cobs, clone, clone.Pending(), c.key)
			if err != nil || !sameIntent(got, c.in) {
				t.Fatalf("auto %v: key %s on a clone: %+v %v, want %+v", auto, c.key, got, err, c.in)
			}
			if CandidateLabel(d, c.in) == "" {
				t.Fatalf("auto %v: empty label for %s", auto, c.key)
			}
		}
		t.Logf("auto %v: %d candidates", auto, len(cands))
	}
}

// sameIntent compares the parts of an intent a candidate sets (a nil and
// an empty Rest are the same answer).
func sameIntent(a, b decision.Intent) bool {
	return a.Seq == b.Seq && a.Player == b.Player && reflect.DeepEqual(a.Choices, b.Choices) &&
		len(a.Rest) == 0 && len(b.Rest) == 0 && reflect.DeepEqual(a.Payment, b.Payment)
}

// NameKeys rewrites only references the root observation did not show:
// with the whole decision observed at the root nothing changes; with none
// of it observed every object reference becomes its card name.
func TestNameKeysNameLateObjects(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 4, 6000)
	obs := searchprobe.NewCollector(0)
	cands, _, _, ok, _ := enumerateCut(obs, e, d, bot, AllKinds(), BenchCandidateLimit, false)
	if !ok {
		t.Fatal("no candidates")
	}
	if got := nameKeys(obs, e, d, cands, obs.Introduced()); !reflect.DeepEqual(got, cands) {
		t.Fatal("keys of root-observed objects changed")
	}
	named := nameKeys(obs, e, d, cands, 0)
	renamed := 0
	for _, c := range named {
		orig, err := obs.Actions(d, c.in)
		if err != nil {
			t.Fatal(err)
		}
		if orig[0].Obj == 0 {
			continue // pass names no object
		}
		acts, err := searchprobe.ParseActionsKey([]byte(c.key))
		if err != nil || len(acts) != 1 || acts[0].Obj != 0 || !strings.Contains(acts[0].Value, "|object=") {
			t.Fatalf("object candidate key %+v (%v) is not in the name form", acts, err)
		}
		renamed++
	}
	if renamed == 0 {
		t.Fatal("nothing renamed")
	}
}
