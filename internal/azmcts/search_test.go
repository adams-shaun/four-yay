package azmcts

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

const testSeed = uint64(30000000)

func testConfig(t testing.TB, a, b string, seed uint64) rules.Config {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	da, err := testutil.LoadRepoDeck(reg, a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testutil.LoadRepoDeck(reg, b)
	if err != nil {
		t.Fatal(err)
	}
	return rules.Config{Seed: seed, Names: []string{a, b}, Decks: [][]*cards.Card{da, db}, Tokens: reg.Tokens}
}

var errNoPosition = errors.New("no searchable seat-0 decision")

// findPosition plays the default bot for both seats from genesis until seat
// 0 faces a decision Search would search (a searched kind with >= 2
// candidates; of kind want unless want is "") at turn >= minTurn, and returns
// the engine there with the bot's answer, unsubmitted.
func findPosition(cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent, error) {
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < maxSteps && !e.G.Over; steps++ {
		d := e.Pending()
		if d == nil {
			return nil, nil, decision.Intent{}, fmt.Errorf("no pending decision at step %d", steps)
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 && e.G.Turn >= minTurn && (want == "" || d.Kind == want) {
			if _, _, ok := enumerate(searchprobe.NewCollector(0), e, d, in, AllKinds(), DefaultOptions().Limit); ok {
				return e, d, in, nil
			}
		}
		if err := e.Submit(in); err != nil {
			return nil, nil, decision.Intent{}, fmt.Errorf("step %d submit: %w", steps, err)
		}
	}
	return nil, nil, decision.Intent{}, errNoPosition
}

func botPosition(t testing.TB, cfg rules.Config, want decision.Kind, minTurn int32, maxSteps int) (*rules.Engine, *decision.Decision, decision.Intent) {
	t.Helper()
	e, d, in, err := findPosition(cfg, want, minTurn, maxSteps)
	if err != nil {
		t.Fatalf("seed %d, kind %q, turn >= %d: %v", cfg.Seed, want, minTurn, err)
	}
	return e, d, in
}

func searchAt(t testing.TB, e *rules.Engine, d *decision.Decision, bot decision.Intent, net *policynet.Model, opts Options) Result {
	t.Helper()
	obs := searchprobe.NewCollector(d.Player)
	src, err := newTestClairvoyant(e, obs)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, src, net, opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// checkResult asserts the invariants of a Search that built a tree over
// clairvoyant worlds with no failure.
func checkResult(t *testing.T, d *decision.Decision, bot decision.Intent, res Result, sims int) {
	t.Helper()
	n := len(res.Candidates)
	if n < 2 || len(res.Keys) != n || len(res.Prior) != n || len(res.Visits) != n || len(res.Q) != n {
		t.Fatalf("shape: %d candidates, %d keys, %d prior, %d visits, %d Q", n, len(res.Keys), len(res.Prior), len(res.Visits), len(res.Q))
	}
	st := res.Stats
	if st.Searched != 1 || st.Simulations != sims || st.Completed != sims {
		t.Fatalf("stats %+v, want %d completed simulations", st, sims)
	}
	var byKind Stats
	byKind.KindSearched[kindIndex(res.Kind)] = 1
	if st.KindSearched != byKind.KindSearched || st.KindSkipped != byKind.KindSkipped || st.PrioritySkipped != byKind.PrioritySkipped {
		t.Fatalf("breakdown %v / %v / %v, want one %s search and no skip", st.KindSearched, st.KindSkipped, st.PrioritySkipped, res.Kind)
	}
	sum := 0
	for _, v := range res.Visits {
		sum += v
	}
	if sum != st.Completed {
		t.Fatalf("root visits sum to %d, completed %d", sum, st.Completed)
	}
	if st.Terminal+st.StepCapped+st.Expanded != st.Completed {
		t.Fatalf("terminal %d + capped %d + expanded %d != completed %d", st.Terminal, st.StepCapped, st.Expanded, st.Completed)
	}
	psum := 0.0
	for _, p := range res.Prior {
		psum += p
	}
	if math.Abs(psum-1) > 1e-9 {
		t.Fatalf("prior sums to %v", psum)
	}
	if res.Choice < 0 || res.Choice >= n {
		t.Fatalf("choice %d of %d", res.Choice, n)
	}
	if err := d.Validate(res.Intent); err != nil {
		t.Fatalf("chosen intent invalid: %v", err)
	}
	if res.Choice == 0 && !reflect.DeepEqual(res.Intent, bot) {
		t.Fatalf("choice 0 played %+v, want the bot's own intent %+v", res.Intent, bot)
	}
	if res.RootValue < 0 || res.RootValue > 1 {
		t.Fatalf("root value %v", res.RootValue)
	}
}

func TestSearchRealDeckPriority(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	head, draws, turn := e.L.Head(), e.RNGDraws(), e.G.Turn
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 7
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 12)
	if res.Kind != "priority" {
		t.Fatalf("kind %q", res.Kind)
	}
	if e.L.Head() != head || e.RNGDraws() != draws || e.G.Turn != turn || e.Pending() != d {
		t.Fatal("Search moved the real engine")
	}
}

func TestSearchRealDeckAttackers(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 12, 8
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 12)
	if res.Kind != "attackers" {
		t.Fatalf("kind %q", res.Kind)
	}
}

// Spec §2/§4: the same seed and checkpoint give a byte-identical choice,
// in eval and in generation mode.
func TestSearchIsDeterministic(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 16, 99
	if a, b := searchAt(t, e, d, bot, nil, opts), searchAt(t, e, d, bot, nil, opts); !reflect.DeepEqual(a, b) {
		t.Fatalf("eval searches differ:\n%+v\n%+v", a, b)
	}
	opts.Noise, opts.Sample = true, true
	if a, b := searchAt(t, e, d, bot, nil, opts), searchAt(t, e, d, bot, nil, opts); !reflect.DeepEqual(a, b) {
		t.Fatalf("generation searches differ:\n%+v\n%+v", a, b)
	}
}

// A network with a value head drives both the prior and the leaf.
func TestSearchWithANetworkLeafAndPrior(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KAttackers, 0, 3000)
	m := policynet.NewModel(policynet.TableRows, 1, 2, rand.New(rand.NewPCG(11, 12)))
	m.InitValue(2, rand.New(rand.NewPCG(13, 14)))
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 8, 5
	res := searchAt(t, e, d, bot, m, opts)
	checkResult(t, d, bot, res, 8)
	if res.Stats.PriorFallbacks != 0 {
		t.Fatalf("prior fell back %d times", res.Stats.PriorFallbacks)
	}
	for _, p := range res.Prior {
		if p <= 0 {
			t.Fatalf("network prior %v has a non-positive entry", res.Prior)
		}
	}
	if again := searchAt(t, e, d, bot, m, opts); !reflect.DeepEqual(res, again) {
		t.Fatal("a network search is not deterministic")
	}
}

// hypSource hands out hypothetical worlds (fresh future chance per
// simulation), the shape ticket 5's sampled worlds take.
type hypSource struct {
	e   *rules.Engine
	obs *searchprobe.Collector
}

func (h hypSource) World(sim int) (World, error) {
	return World{Engine: h.e.CloneHypothetical(uint64(sim) + 1), Observer: h.obs.Clone(), Hypothetical: true}, nil
}

func TestSearchHypotheticalWorlds(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	obs := searchprobe.NewCollector(0)
	opts := DefaultOptions()
	opts.Sims, opts.Seed = 8, 3
	res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, hypSource{e: e, obs: obs}, nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, d, bot, res, 8)
}

type fixedSource struct {
	w   World
	err error
}

func (f fixedSource) World(int) (World, error) { return f.w, f.err }

// Review Focus 3: a world that is not at the root decision is discarded and
// counted; when every simulation fails the bot's answer is played.
func TestSearchBadWorldPlaysTheBot(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	past := e.Clone()
	if err := past.Submit(bot); err != nil {
		t.Fatal(err)
	}
	opts := DefaultOptions()
	opts.Sims = 5
	for _, tc := range []struct {
		name  string
		src   WorldSource
		count func(Stats) int
	}{
		{"one intent past the root", fixedSource{w: World{Engine: past, Observer: searchprobe.NewCollector(0)}}, func(s Stats) int { return s.BadWorlds }},
		{"no engine", fixedSource{w: World{Observer: searchprobe.NewCollector(0)}}, func(s Stats) int { return s.BadWorlds }},
		{"no world", fixedSource{err: fmt.Errorf("%w: sampler starved", ErrNoWorld)}, func(s Stats) int { return s.NoWorld }},
	} {
		obs := searchprobe.NewCollector(0)
		res, err := Search(Root{Engine: e, Decision: d, Bot: bot, Observer: obs}, tc.src, nil, opts)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.count(res.Stats) != 5 || res.Stats.Completed != 0 || res.Stats.AllFailed != 1 {
			t.Errorf("%s: stats %+v", tc.name, res.Stats)
		}
		if res.Choice != 0 || !reflect.DeepEqual(res.Intent, bot) {
			t.Errorf("%s: played %+v, want the bot's answer", tc.name, res.Intent)
		}
	}
}

// Review Focus 5 on the real engine: no walk submits more than MaxSteps.
func TestSearchStepCapBoundsEveryWalk(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e, d, bot := botPosition(t, cfg, decision.KPriority, 0, 2000)
	opts := DefaultOptions()
	opts.Sims, opts.MaxSteps = 8, 1
	res := searchAt(t, e, d, bot, nil, opts)
	checkResult(t, d, bot, res, 8)
	if res.Stats.EnvSteps > res.Stats.Simulations*opts.MaxSteps {
		t.Fatalf("env steps %d exceed %d simulations x cap %d", res.Stats.EnvSteps, res.Stats.Simulations, opts.MaxSteps)
	}
}

type countingSource struct{ calls int }

func (c *countingSource) World(int) (World, error) {
	c.calls++
	return World{}, ErrNoWorld
}

// Review Focus 1 and 4: a priority the bot answers with a land play is not a
// node; the bot's own intent is played and the source is never asked.
func TestSearchSkipsWhenTheBotPlaysALand(t *testing.T) {
	cfg := testConfig(t, "mono-red-prowess", "mono-blue-tempo", testSeed)
	e := rules.New(cfg)
	e.Advance()
	rngs := searchprobe.BotRandoms(cfg.Seed, 2)
	board := botpolicy.NewBoard(2)
	for steps := 0; steps < 2000 && !e.G.Over; steps++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no pending decision at step %d", steps)
		}
		in := botpolicy.Decide(botpolicy.BoardFromGameInto(e.G, e, d.Player, &board), d, rngs[d.Player])
		if d.Player == 0 && d.Kind == decision.KPriority && len(in.Choices) == 1 && d.Options[in.Choices[0]].Kind == "play_land" {
			src := &countingSource{}
			res, err := Search(Root{Engine: e, Decision: d, Bot: in, Observer: searchprobe.NewCollector(0)}, src, nil, DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			if res.Kind != "priority" || res.Stats.Skipped != 1 || res.Stats.Searched != 0 {
				t.Fatalf("result %+v, want a skipped priority", res)
			}
			// A land play is outside the candidate vocabulary: too few
			// candidates, filed under the bot's play_land answer.
			if res.Stats.KindSkipped[KindPriority][SkipFewCandidates] != 1 || res.Stats.PrioritySkipped[BasePlayLand][SkipFewCandidates] != 1 {
				t.Fatalf("breakdown %v / %v, want one few-candidates skip under play_land", res.Stats.KindSkipped, res.Stats.PrioritySkipped)
			}
			if !reflect.DeepEqual(res.Intent, in) || src.calls != 0 {
				t.Fatalf("played %+v with %d world calls, want the bot's land and none", res.Intent, src.calls)
			}
			return
		}
		if err := e.Submit(in); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("seat 0 never played a land")
}

func TestValidateRefusesUnusableOptionsAndNetworks(t *testing.T) {
	for _, o := range []Options{
		func() Options { o := DefaultOptions(); o.CPUCT = 0; return o }(),
		func() Options { o := DefaultOptions(); o.FPU = -1; return o }(),
		func() Options { o := DefaultOptions(); o.Limit = 1; return o }(),
		func() Options { o := DefaultOptions(); o.MaxSteps = 0; return o }(),
		func() Options { o := DefaultOptions(); o.DirichletAlpha = 0; return o }(),
		func() Options { o := DefaultOptions(); o.DirichletEps = 2; return o }(),
		func() Options { o := DefaultOptions(); o.Kinds = Kinds{}; return o }(),
	} {
		if o.Validate(nil) == nil {
			t.Errorf("options %+v accepted", o)
		}
	}
	if err := DefaultOptions().Validate(&policynet.Model{}); err == nil {
		t.Error("a checkpoint without a value head accepted")
	}
	if err := DefaultOptions().Validate(&policynet.Model{ValueHidden: 1, Features: policynet.FeaturesMZOppHand}); err == nil {
		t.Error("an oracle feature set accepted")
	}
	if err := DefaultOptions().Validate(nil); err != nil {
		t.Errorf("defaults refused: %v", err)
	}
}
