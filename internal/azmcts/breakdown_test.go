package azmcts

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
)

// Every skip names its reason, so the stage-0 report can tell an auto-pay
// answer, a decision with too few candidates and a translation failure apart.
func TestEnumerateWhyNamesTheSkipReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		obs  *searchprobe.Collector
		d    *decision.Decision
		bot  decision.Intent
		want SkipReason
	}{
		{"payment", searchprobe.NewCollector(0), passOrAbility(9),
			decision.Intent{Seq: 9, Player: 0, Payment: &decision.PaymentSelection{}}, SkipPayment},
		{"pass only", searchprobe.NewCollector(0),
			&decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
				Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}},
			decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}, SkipFewCandidates},
		{"no attacker", searchprobe.NewCollector(0), &decision.Decision{Seq: 8, Player: 0, Kind: decision.KAttackers},
			decision.Intent{Seq: 8, Player: 0}, SkipFewCandidates},
		{"another seat's collector", searchprobe.NewCollector(1), passOrAbility(10),
			decision.Intent{Seq: 10, Player: 0, Choices: []int{1}}, SkipTranslate},
	} {
		_, kind, why, ok := enumerateWhy(tc.obs, nil, tc.d, tc.bot, AllKinds(), 6)
		if ok || kind == "" || why != tc.want {
			t.Errorf("%s: ok %v kind %q why %v, want a skip for %v", tc.name, ok, kind, why, tc.want)
		}
	}
}

// The bot's answer at a priority decision is classified by the option kind
// it picks (spec follow-up: the manual bot taps mana with "activate" before
// it casts, and those decisions are never searched).
func TestPriorityBaseClassifiesTheBotsAnswer(t *testing.T) {
	d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: []decision.Option{
		{Index: 0, Kind: "cast"}, {Index: 1, Kind: "ability"}, {Index: 2, Kind: "pass"},
		{Index: 3, Kind: "play_land"}, {Index: 4, Kind: "activate"}, {Index: 5, Kind: "turn_face_up"},
	}}
	for _, tc := range []struct {
		in   decision.Intent
		want BaseKind
	}{
		{decision.Intent{Choices: []int{0}}, BaseCast},
		{decision.Intent{Choices: []int{1}}, BaseAbility},
		{decision.Intent{Choices: []int{2}}, BasePass},
		{decision.Intent{Choices: []int{3}}, BasePlayLand},
		{decision.Intent{Choices: []int{4}}, BaseActivate},
		{decision.Intent{Payment: &decision.PaymentSelection{}}, BasePayment},
		{decision.Intent{Choices: []int{5}}, BaseOther},
		{decision.Intent{Choices: []int{9}}, BaseOther},
		{decision.Intent{}, BaseOther},
	} {
		if got := priorityBase(d, tc.in); got != tc.want {
			t.Errorf("priorityBase(%+v) = %v, want %v", tc.in, got, tc.want)
		}
	}
	for i, n := range BaseKindNames {
		if n == "" {
			t.Errorf("base kind %d has no name", i)
		}
	}
}

// Search records a skip under its kind and reason, and a skipped priority
// also under the bot's answer; a searched decision under its kind.
func TestSearchRecordsTheBreakdown(t *testing.T) {
	d := &decision.Decision{Seq: 7, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "pass", Label: "Pass"}}}
	bot := decision.Intent{Seq: 7, Player: 0, Choices: []int{0}}
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, _, _ := botPosition(t, cfg, decision.KPriority, 0, 2000)
	res, err := Search(context.Background(), Root{Engine: e, Decision: d, Bot: bot, Observer: searchprobe.NewCollector(0)}, nil, nil, DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	var want Stats
	want.Skipped = 1
	want.KindSkipped[KindPriority][SkipFewCandidates] = 1
	want.PrioritySkipped[BasePass][SkipFewCandidates] = 1
	if res.Stats != want {
		t.Fatalf("stats %+v, want %+v", res.Stats, want)
	}
}

// leafEnv is a one-point env whose Leaf fails from its failFrom-th call on.
type leafEnv struct {
	calls, failFrom int
}

func (l *leafEnv) Root() *Point { return &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}} }

func (l *leafEnv) Play(Key) (*Point, error) { return nil, nil }

func (l *leafEnv) Leaf() Leaf {
	l.calls++
	if l.calls >= l.failFrom {
		return Leaf{Err: fmt.Errorf("%w: leaf evaluator", ErrPanic)}
	}
	return Leaf{V: 0.75, Terminal: true}
}

// leafSource hands every simulation the same leafEnv, so its Leaf call count
// runs across simulations.
type leafSource struct{ env *leafEnv }

func (s leafSource) Env(int) (Env, error) { return s.env, nil }

// A leaf that fails -- at the root's own evaluation or at the walk's end --
// discards the simulation, counted by its class, and commits nothing.
func TestLeafErrorDiscardsTheSimulation(t *testing.T) {
	root := &Point{Keys: []Key{"a", "b"}, Prior: []float64{0.5, 0.5}}
	for _, failFrom := range []int{1, 2} {
		var st Stats
		res, err := RunTree(context.Background(), root, leafSource{env: &leafEnv{failFrom: failFrom}}, treeOpts(3), &st)
		if err != nil {
			t.Fatal(err)
		}
		if st.Panics != 3 || st.Completed != 0 || st.Terminal != 0 {
			t.Errorf("failFrom %d: stats %+v, want 3 panics and nothing completed", failFrom, st)
		}
		if res.Visits[0]+res.Visits[1] != 0 || res.Avail[0]+res.Avail[1] != 0 {
			t.Errorf("failFrom %d: a failed leaf committed visits %v avail %v", failFrom, res.Visits, res.Avail)
		}
	}
}

// brokenNet has a value head but no weights: every forward pass panics, in
// the prior (priors -> Score) and in the leaf (leafValue -> Value).
func brokenNet() *policynet.Model { return &policynet.Model{ValueHidden: 4} }

// A panic anywhere on the env's own path -- here the network prior at the
// walk's next searched point, and the leaf evaluator -- is recovered into
// ErrPanic rather than escaping into the seat.
func TestEnvRecoversPanicsOutsideSubmit(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	obs := searchprobe.NewCollector(d.Player)
	cands, _, ok := enumerate(obs, e, d, bot, AllKinds(), 6)
	if !ok {
		t.Fatal("the root position is not searchable")
	}
	keys := make([]Key, len(cands))
	for i, c := range cands {
		keys[i] = c.key
	}
	wc := &walkConfig{
		net: brokenNet(), kinds: AllKinds(), limit: 6, maxSteps: 1000, envSeed: 1, actor: d.Player,
		root: &Point{Keys: keys, Prior: uniform(len(keys))}, rootCands: cands, rootDec: d, stats: &Stats{},
	}
	env, err := newEngineEnv(World{Engine: e.Clone(), Observer: obs.Clone()}, wc)
	if err != nil {
		t.Fatal(err)
	}
	if l := env.Leaf(); !errors.Is(l.Err, ErrPanic) {
		t.Fatalf("leaf with a broken network: %+v, want ErrPanic", l)
	}
	if _, err := env.Play(keys[0]); !errors.Is(err, ErrPanic) {
		t.Fatalf("play into a broken network prior: %v, want ErrPanic", err)
	}
}

// The step cap bounds bot submits only: a walk that reaches the searching
// seat's next searched decision exactly at MaxSteps expands it instead of
// counting as capped.
func TestStepCapLetsAPointReachedAtTheCapExpand(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	for minTurn := int32(0); minTurn < 12; minTurn++ {
		e, d, bot := botPosition(t, cfg, "", minTurn, 6000)
		opts := DefaultOptions()
		opts.Sims = 1
		free := searchAt(t, e, d, bot, nil, opts)
		if free.Stats.Expanded != 1 || free.Stats.EnvSteps < 1 {
			continue // the one walk ended the game, or met a point at once
		}
		opts.MaxSteps = free.Stats.EnvSteps
		at := searchAt(t, e, d, bot, nil, opts)
		if at.Stats.Expanded != 1 || at.Stats.StepCapped != 0 || at.Stats.EnvSteps != free.Stats.EnvSteps {
			t.Fatalf("cap %d = the walk's own length: stats %+v, want it expanded, not capped", opts.MaxSteps, at.Stats)
		}
		opts.MaxSteps = free.Stats.EnvSteps - 1
		if opts.MaxSteps >= 1 {
			if below := searchAt(t, e, d, bot, nil, opts); below.Stats.StepCapped != 1 || below.Stats.Expanded != 0 {
				t.Fatalf("cap %d below the walk's length: stats %+v, want it capped", opts.MaxSteps, below.Stats)
			}
		}
		return
	}
	t.Fatal("no position in turns 0-11 walked to an expansion with at least one env step")
}
