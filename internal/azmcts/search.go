package azmcts

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Root is the real decision being searched.
type Root struct {
	// Engine is the real engine. Search reads it (the actor's own decision,
	// its board for the blockers guard, its view for the prior) and never
	// submits to it.
	Engine *rules.Engine
	// Decision is Engine's pending decision, the searching seat's own.
	Decision *decision.Decision
	// Bot is the wrapped bot's answer: candidate 0, the tie-winner, and the
	// answer on every failure path.
	Bot decision.Intent
	// Observer is a FRESH collector for Decision.Player -- never a
	// searchseat.Feed's, whose Capture stream ObserveDecision would perturb.
	// Every world's Observer must be a clone of it taken after Search starts.
	Observer *searchprobe.Collector

	// The full root (the search benchmark's): set any of these and a
	// priority root under Options.AutoPayment is searched over the
	// vocabulary plus Macros even when Bot is outside the vocabulary (a mana
	// activation, a land play), instead of being skipped. Macros are root
	// candidates played as several submits (Macro); BotKey names the bot's
	// candidate when Bot itself is outside the vocabulary (a macro the bot
	// was pursuing); NoBot alone asks for the full root with no macros. With
	// neither Bot's candidate nor BotKey's there is no bot candidate and
	// Pass is candidate 0. In-walk decisions are unchanged.
	Macros []Macro
	BotKey Key
	NoBot  bool
}

// Result is one Search. Candidates, Keys, Labels, Visits, Avail, Prior and
// Q are parallel; index 0 is the bot's answer.
type Result struct {
	Kind       string // "", or the searched kind
	Candidates []decision.Intent
	Keys       []Key
	Labels     []string // CandidateLabel of each candidate on the root decision
	Visits     []int
	Avail      []int     // simulations in which each root candidate was available
	Prior      []float64 // the prior before any root noise
	Q          []float64
	RootValue  float64
	Choice     int
	Intent     decision.Intent // the answer to play
	Stats      Stats
}

// RootRow is one root child of a Search: its semantic key, a
// human-readable label, its visits, mean backed-up value (0 when
// unvisited), availability count and prior.
type RootRow struct {
	Key    Key     `json:"key"`
	Label  string  `json:"label"`
	Visits int     `json:"visits"`
	Q      float64 `json:"q"`
	Avail  int     `json:"avail"`
	Prior  float64 `json:"prior"`
}

// RootTable is r's root children in candidate order (nil when no tree was
// built).
func (r Result) RootTable() []RootRow {
	if len(r.Visits) != len(r.Keys) {
		return nil
	}
	out := make([]RootRow, len(r.Keys))
	for i, k := range r.Keys {
		out[i] = RootRow{Key: k, Visits: r.Visits[i]}
		if i < len(r.Labels) {
			out[i].Label = r.Labels[i]
		}
		if i < len(r.Q) {
			out[i].Q = r.Q[i]
		}
		if i < len(r.Avail) {
			out[i].Avail = r.Avail[i]
		}
		if i < len(r.Prior) {
			out[i].Prior = r.Prior[i]
		}
	}
	return out
}

// Validate refuses options and networks Search must not run with.
func (o Options) Validate(net *policynet.Model) error {
	switch {
	case o.CPUCT <= 0:
		return fmt.Errorf("azmcts: CPUCT %g must be > 0", o.CPUCT)
	case o.FPU < 0:
		return fmt.Errorf("azmcts: FPU %g must be >= 0", o.FPU)
	case o.Limit < 2:
		return fmt.Errorf("azmcts: candidate limit %d must be >= 2", o.Limit)
	case o.MaxSteps < 1:
		return fmt.Errorf("azmcts: step cap %d must be >= 1", o.MaxSteps)
	case o.DirichletAlpha <= 0:
		return fmt.Errorf("azmcts: Dirichlet alpha %g must be > 0", o.DirichletAlpha)
	case o.DirichletEps < 0 || o.DirichletEps > 1:
		return fmt.Errorf("azmcts: Dirichlet epsilon %g must be in [0,1]", o.DirichletEps)
	case o.CachedWorlds < 0:
		return fmt.Errorf("azmcts: cached worlds %d must be >= 0 (0 is off)", o.CachedWorlds)
	case o.NodeCache < 0:
		return fmt.Errorf("azmcts: node cache %d must be >= 0 (0 is off)", o.NodeCache)
	case o.PriorTopK < 0 || o.PriorTopK == 1:
		// One kept candidate is the bot's alone: nothing would be searched.
		return fmt.Errorf("azmcts: prior top-k %d must be 0 (off) or >= 2", o.PriorTopK)
	case o.Kinds == (Kinds{}):
		return errors.New("azmcts: no searched decision kinds")
	case o.Discount < 0 || o.Discount > 1 || o.Discount != o.Discount:
		return fmt.Errorf("azmcts: discount %g must be in [0,1] (0 and 1 are off)", o.Discount)
	case int(o.DiscountUnit) >= len(DiscountUnitNames):
		return fmt.Errorf("azmcts: unknown discount unit %d", o.DiscountUnit)
	case o.AbsoluteUnvisitedQ && (o.UnvisitedQ < 0 || o.UnvisitedQ > 1 || o.UnvisitedQ != o.UnvisitedQ):
		return fmt.Errorf("azmcts: unvisited Q %g must be in [0,1]", o.UnvisitedQ)
	}
	if net != nil {
		if !net.HasValue() {
			return errors.New("azmcts: checkpoint has no value head; the value head is the search's leaf (spec §1), so a policy-only checkpoint cannot drive it")
		}
		if net.Features.Diagnostic() {
			return fmt.Errorf("azmcts: checkpoint feature set %s reads hidden information; the network must read the searching seat's redacted view (spec §1)", net.Features)
		}
	}
	return nil
}

// priorTopK is the PriorTopK in effect with net: 0 unless a network supplies
// the prior (a net, UniformPrior off).
func (o Options) priorTopK(net *policynet.Model) int {
	if o.PriorTopK <= 0 || net == nil || o.UniformPrior {
		return 0
	}
	return o.PriorTopK
}

// countTopK counts one PriorTopK point in st: ranked, and cut when before
// (rankedPrior's) is positive.
func countTopK(st *Stats, ranked bool, before int) {
	if ranked {
		st.PriorTopKPoints++
	}
	if before > 0 {
		st.PriorTopKCuts++
		st.PriorTopKBefore += before
	}
}

// Search runs the tree at the searching seat's current decision (spec §1-§2)
// and returns the answer to play. A decision that is not searched -- not a
// searched kind, fewer than two candidates, a bot answer outside the
// candidate vocabulary, Sims <= 0 -- returns the bot's intent without asking
// src for a world. If every simulation fails, the bot's intent is played and
// Stats.AllFailed is 1. The error return is reserved for misconfiguration
// (invalid options or network, a missing root field or source).
//
// net nil is generation 0: a uniform prior and the heuristic leaf. With a
// live ctx the result is a pure function of (root position, bot answer, net,
// opts): the same Seed gives a byte-identical Result.
// ctx is the WALL-CLOCK BAIL-OUT, an escape hatch a production host arms
// with a per-decision deadline (bots.Options.DecisionDeadline) so a search
// cannot run to completion regardless of load. A ctx that is done -- already
// at the call, or between simulations -- stops the tree where it is and the
// bot's intent is played exactly as on a search that never built;
// Stats.DeadlineHits counts it and the partial tree's choice is never taken
// (how many simulations complete under load is not reproducible, so arming
// it gives up the determinism contract below by construction). A live
// context -- context.Background(), what every benchmark, replay and
// determinism test passes -- never arms it.
func Search(ctx context.Context, root Root, src WorldSource, net *policynet.Model, opts Options) (Result, error) {
	res := Result{Intent: root.Bot}
	if err := opts.Validate(net); err != nil {
		return res, err
	}
	if root.Engine == nil || root.Decision == nil || root.Observer == nil {
		return res, errors.New("azmcts: Search needs the root engine, decision and observer")
	}
	var envBoard, enumBoard boardScratch
	views := searchViews.Get().(*viewScratch)
	defer searchViews.Put(views)
	// Under PriorTopK the network, not the enumeration order, chooses the
	// candidates: every point enumerates in full and rankedPrior cuts.
	topK, limit := opts.priorTopK(net), opts.Limit
	if topK > 0 {
		limit = BenchCandidateLimit
	}
	cands, kind, why, ok, cut, botFound := rootCands(root.Observer, root, opts.Kinds, limit, opts.AutoPayment, &enumBoard)
	res.Kind = kind
	if cut {
		res.Stats.Truncated, res.Stats.RootTruncated = 1, 1
	}
	if !ok {
		if k := kindIndex(kind); k >= 0 {
			res.Stats.Skipped = 1
			res.Stats.KindSkipped[k][why]++
			if k == KindPriority {
				res.Stats.PrioritySkipped[priorityBase(root.Decision, root.Bot)][why]++
			}
		}
		return res, nil
	}
	priorNet := net
	if opts.UniformPrior {
		priorNet = nil
	}
	// The prior comes before the Result's candidate lists: under PriorTopK
	// it decides which candidates they hold.
	cands, prior, fell, before := rankedPrior(priorNet, topK, root.Engine, root.Decision, root.Bot, kind, cands, &views.prior)
	if fell {
		res.Stats.PriorFallbacks++
	}
	countTopK(&res.Stats, topK > 0, before)
	res.Candidates = make([]decision.Intent, len(cands))
	res.Keys = make([]Key, len(cands))
	res.Labels = make([]string, len(cands))
	for i, c := range cands {
		res.Candidates[i], res.Keys[i] = c.in, c.key
		res.Labels[i] = CandidateLabel(root.Decision, c.in)
		if c.macro != nil {
			res.Labels[i] = c.macro.Label
		}
	}
	res.Prior = prior
	if opts.Sims <= 0 {
		return res, nil
	}
	if src == nil {
		return res, errors.New("azmcts: Search needs a world source")
	}
	res.Stats.Searched = 1
	res.Stats.KindSearched[kindIndex(kind)]++
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	treePrior := prior
	if opts.Noise {
		treePrior = noisyPrior(prior, rng, opts.DirichletAlpha, opts.DirichletEps)
	}
	rootPt := &Point{Keys: res.Keys, Prior: treePrior}
	cfg := &walkConfig{
		net: net, heuristicLeaf: opts.HeuristicLeaf, kinds: opts.Kinds, limit: opts.Limit, priorTopK: topK, maxSteps: opts.MaxSteps,
		envSeed: splitmix(opts.Seed ^ 0x656e762d73656564), actor: root.Decision.Player, autoPayment: opts.AutoPayment, skipPass: opts.SkipPass,
		uniformPrior: opts.UniformPrior, rootPerWorld: opts.RootPerWorld,
		nameKeys: opts.NameKeys, rootRefs: root.Observer.Introduced(),
		root: rootPt, rootCands: cands, rootDec: root.Decision, stats: &res.Stats,
		envBoard: &envBoard, enumBoard: &enumBoard, views: views,
	}
	if opts.OpponentNodes {
		cfg.oppNodes = true
		cfg.oppForks = make([]*searchprobe.Forker, len(root.Engine.G.Players))
		for p := range cfg.oppForks {
			if pl := state.PlayerID(p); pl != cfg.actor {
				cfg.oppForks[p] = searchprobe.NewCollector(pl).Forker()
			}
		}
	}
	var tr TreeResult
	var err error
	if rs, ok := src.(*RedealSource); ok && opts.CachedWorlds > 0 && !opts.RootPerWorld {
		tr, err = runCachedWorlds(ctx, rootPt, rs, cfg, opts, &res.Stats)
	} else {
		var envs EnvSource = &worldEnvs{src: src, cfg: cfg}
		if opts.NodeCache > 0 && isFixed(src) {
			envs = &fixedEnvs{worldEnvs: envs.(*worldEnvs)}
		}
		tr, err = RunTree(ctx, rootPt, envs, opts, &res.Stats)
	}
	if err != nil {
		return res, err
	}
	if res.Stats.DeadlineHits > 0 {
		// The armed bail-out fired (ctx done at the call or between
		// simulations): the tree stopped where it was and the bot's answer
		// is played -- the partial tree's choice is never taken.
		return res, nil
	}
	res.Visits, res.Q, res.Avail, res.RootValue = tr.Visits, tr.Q, tr.Avail, tr.RootValue
	if res.Stats.Completed == 0 {
		res.Stats.AllFailed = 1
		return res, nil
	}
	res.Choice = choose(res.Visits, opts.Sample, rng)
	if res.Choice != 0 || !botFound {
		res.Intent = cands[res.Choice].in
	}
	return res, nil
}

// DecisionSeed is the per-decision seed of spec §2: a mix of the seat's seed
// (itself derived from the game seed, internal/bench.RunPairs) and the
// decision's sequence number.
func DecisionSeed(seatSeed, seq uint64) uint64 { return splitmix(seatSeed ^ splitmix(seq)) }

// splitmix is SplitMix64's finaliser.
func splitmix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
