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
}

// Result is one Search. Candidates, Keys, Visits, Prior and Q are parallel;
// index 0 is the bot's answer.
type Result struct {
	Kind       string // "", or the searched kind
	Candidates []decision.Intent
	Keys       []Key
	Visits     []int
	Prior      []float64 // the prior before any root noise
	Q          []float64
	RootValue  float64
	Choice     int
	Intent     decision.Intent // the answer to play
	Stats      Stats
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
	case o.Kinds == (Kinds{}):
		return errors.New("azmcts: no searched decision kinds")
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
	cands, kind, why, ok := enumerateWhyInto(root.Observer, root.Engine, root.Decision, root.Bot, opts.Kinds, opts.Limit, &enumBoard)
	res.Kind = kind
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
	res.Candidates = make([]decision.Intent, len(cands))
	res.Keys = make([]Key, len(cands))
	for i, c := range cands {
		res.Candidates[i], res.Keys[i] = c.in, c.key
	}
	prior, fell := priors(net, root.Engine, root.Decision, root.Bot, kind, cands)
	if fell {
		res.Stats.PriorFallbacks++
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
		net: net, heuristicLeaf: opts.HeuristicLeaf, kinds: opts.Kinds, limit: opts.Limit, maxSteps: opts.MaxSteps,
		envSeed: splitmix(opts.Seed ^ 0x656e762d73656564), actor: root.Decision.Player,
		root: rootPt, rootCands: cands, rootDec: root.Decision, stats: &res.Stats,
		envBoard: &envBoard, enumBoard: &enumBoard,
	}
	tr, err := RunTree(ctx, rootPt, &worldEnvs{src: src, cfg: cfg}, opts, &res.Stats)
	if err != nil {
		return res, err
	}
	if res.Stats.DeadlineHits > 0 {
		// The armed bail-out fired (ctx done at the call or between
		// simulations): the tree stopped where it was and the bot's answer
		// is played -- the partial tree's choice is never taken.
		return res, nil
	}
	res.Visits, res.Q, res.RootValue = tr.Visits, tr.Q, tr.RootValue
	if res.Stats.Completed == 0 {
		res.Stats.AllFailed = 1
		return res, nil
	}
	res.Choice = choose(res.Visits, opts.Sample, rng)
	if res.Choice != 0 {
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
