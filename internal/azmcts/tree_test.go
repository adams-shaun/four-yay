package azmcts

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
)

// fakeNode is one position of a hand-built game tree, addressed by the path
// of keys played from the root ("" is the root, "/a/x" two plies down).
type fakeNode struct {
	keys     []Key
	prior    []float64
	value    float64
	terminal bool
	capped   bool
	err      error // Play into this path always fails with err
	opp      bool  // the opponent's decision (Point.opp)
}

type fakeGame struct {
	nodes    map[string]fakeNode
	failOnce map[string]error     // Play into this path fails once, then plays normally
	rootFor  func(sim int) *Point // per-world root offer (availability); nil = nodes[""]
	oppFlip  map[string]bool      // Play into this path offers the other seat's decision from sim 1 on
}

type fakeEnv struct {
	g    *fakeGame
	sim  int
	path string
}

func (f *fakeEnv) Root() *Point {
	if f.g.rootFor != nil {
		return f.g.rootFor(f.sim)
	}
	n := f.g.nodes[""]
	return &Point{Keys: n.keys, Prior: n.prior, opp: n.opp}
}

func (f *fakeEnv) Play(k Key) (*Point, error) {
	f.path += "/" + string(k)
	if err, ok := f.g.failOnce[f.path]; ok {
		delete(f.g.failOnce, f.path)
		return nil, err
	}
	n, ok := f.g.nodes[f.path]
	if !ok {
		return nil, fmt.Errorf("fake: no node at %q", f.path)
	}
	if n.err != nil {
		return nil, n.err
	}
	if n.terminal || n.capped {
		return nil, nil
	}
	opp := n.opp
	if f.g.oppFlip[f.path] && f.sim > 0 {
		opp = !opp
	}
	return &Point{Keys: n.keys, Prior: n.prior, opp: opp}, nil
}

func (f *fakeEnv) Leaf() Leaf {
	n := f.g.nodes[f.path]
	return Leaf{V: n.value, Terminal: n.terminal, Capped: n.capped}
}

type fakeSource struct {
	g     *fakeGame
	err   error
	calls int
}

func (s *fakeSource) Env(sim int) (Env, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return &fakeEnv{g: s.g, sim: sim}, nil
}

func rootOf(g *fakeGame) *Point {
	n := g.nodes[""]
	return &Point{Keys: n.keys, Prior: n.prior, opp: n.opp}
}

func treeOpts(sims int) Options {
	o := DefaultOptions()
	o.Sims = sims
	return o
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestPUCTScore(t *testing.T) {
	if got := puctScore(0.4, 0.8, 1, 0, 1.5); !near(got, 1.6) {
		t.Errorf("puctScore(0.4, 0.8, 1, 0) = %v, want 1.6", got)
	}
	if got := puctScore(0.25, 0.5, 4, 1, 1.5); !near(got, 1.0) {
		t.Errorf("puctScore(0.25, 0.5, 4, 1) = %v, want 1.0", got)
	}
	if got, want := puctScore(0.7, 0.5, 3, 2, 1.5), 0.7+0.75*math.Sqrt(3)/3; !near(got, want) {
		t.Errorf("puctScore(0.7, 0.5, 3, 2) = %v, want %v", got, want)
	}
}

// Sim 0 evaluates the root (0.5), then picks by U alone: both unvisited
// children read Q = 0.5-0.1 = 0.4, U(a)=1.5*0.2=0.3, U(b)=1.5*0.8=1.2 -> b.
func TestFirstSimulationFollowsThePrior(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.2, 0.8}, value: 0.5},
		"/a": {value: 0.1, terminal: true},
		"/b": {value: 0.9, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(1), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{0, 1}) {
		t.Fatalf("visits %v, want [0 1]", tr.Visits)
	}
	if !near(tr.RootValue, 0.7) {
		t.Fatalf("root value %v, want (0.5+0.9)/2", tr.RootValue)
	}
	if st.Simulations != 1 || st.Completed != 1 || st.Terminal != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// Candidate 0 is the bot's answer and wins every exact tie.
func TestTiesGoToCandidateZero(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a": {value: 0.5, terminal: true},
		"/b": {value: 0.5, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(1), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) {
		t.Fatalf("visits %v, want [1 0]", tr.Visits)
	}
	if c := choose(tr.Visits, false, nil); c != 0 {
		t.Fatalf("choose = %d, want 0", c)
	}
	if c := choose([]int{2, 2}, false, nil); c != 0 {
		t.Fatalf("choose on a visit tie = %d, want 0", c)
	}
	if c := choose([]int{0, 0}, true, rand.New(rand.NewPCG(1, 2))); c != 0 {
		t.Fatalf("choose with no visits = %d, want 0", c)
	}
}

// With FPU 0.1 the unvisited b reads Q = parentQ-0.1 = 0.15 and wins sim 1
// (0.15+0.849 > 0+0.636); with FPU 1.0 it reads -0.75 and a is chosen again.
func TestFirstPlayUrgency(t *testing.T) {
	nodes := map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.6, 0.4}, value: 0.5},
		"/a": {value: 0, terminal: true},
		"/b": {value: 1, terminal: true},
	}
	for _, tc := range []struct {
		fpu  float64
		want []int
	}{{0.1, []int{1, 1}}, {1.0, []int{2, 0}}} {
		g := &fakeGame{nodes: nodes}
		o := treeOpts(2)
		o.FPU = tc.fpu
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, o, &st)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tr.Visits, tc.want) {
			t.Errorf("FPU %v: visits %v, want %v", tc.fpu, tr.Visits, tc.want)
		}
	}
}

// Two levels. sim0 expands a (0.6); sim1 takes b (terminal 0.2); sim2 walks
// a -> x (terminal 1). Root N=4 W=0.5+0.6+0.2+1; edge a N=2 W=1.6.
func TestBackupAlongPath(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":     {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a":   {keys: []Key{"x", "y"}, prior: []float64{0.5, 0.5}, value: 0.6},
		"/a/x": {value: 1, terminal: true},
		"/a/y": {value: 0, terminal: true},
		"/b":   {value: 0.2, terminal: true},
	}}
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(3), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{2, 1}) {
		t.Fatalf("visits %v, want [2 1]", tr.Visits)
	}
	if !near(tr.RootValue, 0.575) || !near(tr.Q[0], 0.8) || !near(tr.Q[1], 0.2) {
		t.Fatalf("root value %v Q %v, want 0.575 [0.8 0.2]", tr.RootValue, tr.Q)
	}
	if st.Completed != 3 || st.Expanded != 1 || st.Terminal != 2 {
		t.Fatalf("stats %+v, want completed 3 expanded 1 terminal 2", st)
	}
}

// Odd worlds do not offer b: b is scored only in worlds offering it (the
// ISMCTS availability rule) and each miss is counted.
func TestAvailabilityCountsOnlyOfferingWorlds(t *testing.T) {
	both := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {value: 0.5},
			"/a": {value: 0.5, terminal: true},
			"/b": {value: 0.5, terminal: true},
		},
		rootFor: func(sim int) *Point {
			if sim%2 == 0 {
				return both
			}
			return &Point{Keys: []Key{"a"}, Prior: []float64{1}}
		},
	}
	var st Stats
	tr, err := RunTree(context.Background(), both, &fakeSource{g: g}, treeOpts(4), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{3, 1}) || !reflect.DeepEqual(tr.Avail, []int{4, 2}) {
		t.Fatalf("visits %v avail %v, want [3 1] [4 2]", tr.Visits, tr.Avail)
	}
	if st.Unavailable != 2 {
		t.Fatalf("unavailable %d, want 2", st.Unavailable)
	}
}

// A key first offered by a later world joins the node with that world's
// prior; Visits stays parallel to the root's own keys.
func TestNewKeyFromALaterWorld(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {value: 0.5},
			"/a": {value: 0.5, terminal: true},
			"/c": {value: 0.9, terminal: true},
		},
		rootFor: func(sim int) *Point {
			if sim == 0 {
				return &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
			}
			return &Point{Keys: []Key{"c"}, Prior: []float64{1}}
		},
	}
	root := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	var st Stats
	tr, err := RunTree(context.Background(), root, &fakeSource{g: g}, treeOpts(2), &st)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) || st.Completed != 2 || st.Unavailable != 2 {
		t.Fatalf("visits %v stats %+v", tr.Visits, st)
	}
	if !near(tr.RootValue, (0.5+0.5+0.9)/3) {
		t.Fatalf("root value %v", tr.RootValue)
	}
}

// A failed simulation changes no visit, value or availability count.
func TestFailedSimulationIsDiscarded(t *testing.T) {
	g := &fakeGame{
		nodes: map[string]fakeNode{
			"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
			"/a": {value: 0.3, terminal: true},
			"/b": {value: 0.8, terminal: true},
		},
		failOnce: map[string]error{"/a": fmt.Errorf("%w: test", ErrChance)},
	}
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(2), &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.ChanceFailures != 1 || st.Completed != 1 || st.Simulations != 2 {
		t.Fatalf("stats %+v", st)
	}
	if !reflect.DeepEqual(tr.Visits, []int{1, 0}) || !reflect.DeepEqual(tr.Avail, []int{1, 1}) || !near(tr.RootValue, 0.4) {
		t.Fatalf("visits %v avail %v root %v", tr.Visits, tr.Avail, tr.RootValue)
	}
}

func TestEveryFailureKindIsCounted(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{"": {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}}}}
	for _, tc := range []struct {
		err   error
		count func(Stats) int
	}{
		{fmt.Errorf("%w: t", ErrNoWorld), func(s Stats) int { return s.NoWorld }},
		{fmt.Errorf("%w: t", ErrBadWorld), func(s Stats) int { return s.BadWorlds }},
		{fmt.Errorf("%w: t", ErrChance), func(s Stats) int { return s.ChanceFailures }},
		{fmt.Errorf("%w: t", ErrPanic), func(s Stats) int { return s.Panics }},
		{errors.New("anything else"), func(s Stats) int { return s.SubmitErrors }},
	} {
		var st Stats
		tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g, err: tc.err}, treeOpts(3), &st)
		if err != nil {
			t.Fatal(err)
		}
		if tc.count(st) != 3 || st.Completed != 0 || !reflect.DeepEqual(tr.Visits, []int{0, 0}) {
			t.Errorf("%v: stats %+v visits %v", tc.err, st, tr.Visits)
		}
	}
}

// Review Focus 5: every walk stops at the cap; visits still decide.
func TestStepCapOnEverySimulation(t *testing.T) {
	g := &fakeGame{nodes: map[string]fakeNode{
		"":   {keys: []Key{"a", "b"}, prior: []float64{0.5, 0.5}, value: 0.5},
		"/a": {value: 0.7, capped: true},
		"/b": {value: 0.3, capped: true},
	}}
	var st Stats
	tr, err := RunTree(context.Background(), rootOf(g), &fakeSource{g: g}, treeOpts(4), &st)
	if err != nil {
		t.Fatal(err)
	}
	if st.StepCapped != 4 || st.Terminal != 0 || st.Expanded != 0 || st.Completed != 4 {
		t.Fatalf("stats %+v, want every simulation capped", st)
	}
	if !reflect.DeepEqual(tr.Visits, []int{3, 1}) || !near(tr.RootValue, 0.58) {
		t.Fatalf("visits %v root %v, want [3 1] 0.58", tr.Visits, tr.RootValue)
	}
}

func TestRunTreeRejectsAMalformedRoot(t *testing.T) {
	var st Stats
	for _, root := range []*Point{nil, {}, {Keys: []Key{"a"}, Prior: []float64{0.5, 0.5}}} {
		if _, err := RunTree(context.Background(), root, &fakeSource{}, treeOpts(1), &st); err == nil {
			t.Errorf("root %+v accepted", root)
		}
	}
}

func TestDirichletNoise(t *testing.T) {
	eta := dirichlet(rand.New(rand.NewPCG(1, 2)), 4, 0.3)
	sum := 0.0
	for _, x := range eta {
		if x < 0 {
			t.Fatalf("negative component %v", eta)
		}
		sum += x
	}
	if !near(sum, 1) {
		t.Fatalf("components sum to %v", sum)
	}
	if again := dirichlet(rand.New(rand.NewPCG(1, 2)), 4, 0.3); !reflect.DeepEqual(eta, again) {
		t.Fatal("the same seed drew different noise")
	}
	rng := rand.New(rand.NewPCG(3, 4))
	mean := 0.0
	for i := 0; i < 2000; i++ {
		mean += dirichlet(rng, 3, 0.3)[0]
	}
	if mean /= 2000; mean < 0.30 || mean > 0.37 {
		t.Fatalf("component mean %v, want about 1/3", mean)
	}
	prior := []float64{0.25, 0.25, 0.25, 0.25}
	p := noisyPrior(prior, rand.New(rand.NewPCG(5, 6)), 0.3, 0.25)
	sum = 0
	for _, x := range p {
		if x < 0.75*0.25-1e-12 {
			t.Fatalf("noisy prior %v fell below (1-eps)*prior", p)
		}
		sum += x
	}
	if !near(sum, 1) {
		t.Fatalf("noisy prior sums to %v", sum)
	}
	if same := noisyPrior(prior, rand.New(rand.NewPCG(5, 6)), 0.3, 0); !reflect.DeepEqual(same, prior) {
		t.Fatalf("eps 0 changed the prior: %v", same)
	}
}

func TestChooseSamplesProportionalToVisits(t *testing.T) {
	if c := choose([]int{1, 3}, false, nil); c != 1 {
		t.Fatalf("argmax = %d, want 1", c)
	}
	rng := rand.New(rand.NewPCG(7, 8))
	ones := 0
	for i := 0; i < 4000; i++ {
		if choose([]int{1, 3}, true, rng) == 1 {
			ones++
		}
	}
	if f := float64(ones) / 4000; f < 0.72 || f > 0.78 {
		t.Fatalf("sampled share of the 3-visit child %v, want about 0.75", f)
	}
}

func TestParseKinds(t *testing.T) {
	k, err := ParseKinds("priority, attackers")
	if err != nil || k != (Kinds{Priority: true, Attackers: true}) {
		t.Fatalf("ParseKinds = %+v, %v", k, err)
	}
	if k, err := ParseKinds("priority,attackers,blockers,target"); err != nil || k != AllKinds() {
		t.Fatalf("all four = %+v, %v", k, err)
	}
	for _, bad := range []string{"", "cast", "priority,"} {
		if _, err := ParseKinds(bad); err == nil {
			t.Errorf("ParseKinds(%q) accepted", bad)
		}
	}
}

// setInts sets every int leaf of v (fields, array elements) to n.
func setInts(v reflect.Value, n int64) {
	switch v.Kind() {
	case reflect.Int:
		v.SetInt(n)
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			setInts(v.Index(i), n)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			setInts(v.Field(i), n)
		}
	}
}

// checkInts reports every int leaf of v that is not want.
func checkInts(t *testing.T, path string, v reflect.Value, want int64) {
	t.Helper()
	switch v.Kind() {
	case reflect.Int:
		if got := v.Int(); got != want {
			t.Errorf("Stats%s = %d after adding 1 twice (Add misses the field)", path, got)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			checkInts(t, fmt.Sprintf("%s[%d]", path, i), v.Index(i), want)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			checkInts(t, path+"."+v.Type().Field(i).Name, v.Field(i), want)
		}
	default:
		t.Errorf("Stats%s has kind %s, which Add cannot sum", path, v.Kind())
	}
}

func TestStatsAddSumsEveryCounter(t *testing.T) {
	var one Stats
	setInts(reflect.ValueOf(&one).Elem(), 1)
	var sum Stats
	sum.Add(one)
	sum.Add(one)
	checkInts(t, "", reflect.ValueOf(sum), 2)
}
