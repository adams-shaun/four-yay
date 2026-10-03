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

	// Reuse is the seat's tree carrier (Options.ReuseTree; required with
	// the switch on, refused without it): the search adopts the stored
	// node that is this position, if any, and stores its own tree back
	// when its move comes from the tree. The caller must then play
	// Result.Choice's candidate (or Result.Intent) before the seat's next
	// search.
	Reuse *Reuse

	// Combat is the answers already given to the earlier creatures of a
	// split declaration (Options.CombatSteps; required nil without it): with
	// the switch on, an attackers or blockers root is creature
	// len(Combat)'s step (Result.Step), and Combat[k] is creature k's answer
	// (the option index its step played, -1 for none). The caller searches
	// each creature in turn and submits CombatDeclaration after the last.
	Combat []int
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
	// Step, set when the root is one creature of a split declaration
	// (Options.CombatSteps), describes it; Intent is then that creature's
	// answer (StepAnswer), never a submit, and Choice 0 is the "no" answer
	// rather than the bot's.
	Step *CombatStep `json:",omitempty"`
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
	case o.NodeCache < 0:
		return fmt.Errorf("azmcts: node cache %d must be >= 0 (0 is off)", o.NodeCache)
	case o.Kinds == (Kinds{}):
		return errors.New("azmcts: no searched decision kinds")
	case o.Discount < 0 || o.Discount > 1 || o.Discount != o.Discount:
		return fmt.Errorf("azmcts: discount %g must be in [0,1] (0 and 1 are off)", o.Discount)
	case int(o.DiscountUnit) >= len(DiscountUnitNames):
		return fmt.Errorf("azmcts: unknown discount unit %d", o.DiscountUnit)
	case o.AbsoluteUnvisitedQ && (o.UnvisitedQ < 0 || o.UnvisitedQ > 1 || o.UnvisitedQ != o.UnvisitedQ):
		return fmt.Errorf("azmcts: unvisited Q %g must be in [0,1]", o.UnvisitedQ)
	case o.OpponentLimit < 0 || o.OpponentLimit == 1:
		return fmt.Errorf("azmcts: opponent candidate limit %d must be 0 (the candidate limit) or >= 2", o.OpponentLimit)
	case o.OpponentNodes && o.RootPerWorld:
		return errors.New("azmcts: opponent nodes need one fixed world; RootPerWorld re-derives the root per world")
	case o.swapFrame && !o.OpponentNodes:
		return errors.New("azmcts: the swapped value frame needs opponent nodes")
	case o.ReuseTree && o.RootPerWorld:
		return errors.New("azmcts: tree reuse needs the one real world; RootPerWorld re-derives the root per world")
	case o.ReuseTree && o.swapFrame:
		return errors.New("azmcts: tree reuse with the swapped value frame is not supported")
	case o.CombatSteps && o.RootPerWorld:
		return errors.New("azmcts: per-creature combat needs one fixed world; RootPerWorld re-derives the root per world")
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
// it gives up the determinism contract below by construction) -- unless
// Options.DeadlineBestChild asks for upstream's best child so far, which is
// then a pure function of the partial tree (Stats.DeadlineBest). A live
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
	if opts.OpponentNodes {
		if err := checkOpponentGame(root); err != nil {
			return res, err
		}
	}
	if opts.ReuseTree != (root.Reuse != nil) {
		return res, errors.New("azmcts: Options.ReuseTree and Root.Reuse go together: the switch needs the seat's carrier, and a carrier needs the switch")
	}
	if root.Combat != nil && !(opts.CombatSteps && combatSearched(root.Decision, opts.Kinds)) {
		return res, errors.New("azmcts: Root.Combat names a creature step, which needs Options.CombatSteps and a searched attackers or blockers root")
	}
	var envBoard, enumBoard boardScratch
	views := searchViews.Get().(*viewScratch)
	defer searchViews.Put(views)
	var (
		cands      []cand
		kind       string
		why        SkipReason
		ok, cut    bool
		botFound   bool
		rootCombat *combatWalk
	)
	if opts.CombatSteps && combatSearched(root.Decision, opts.Kinds) {
		var err error
		cands, kind, why, ok, cut, rootCombat, err = combatRoot(root, opts)
		if err != nil {
			return res, err
		}
		if rootCombat != nil {
			k := len(root.Combat)
			st := rootCombat.plan.steps[k]
			res.Step = &CombatStep{Index: k, Steps: len(rootCombat.plan.steps), Obj: st.obj, Block: rootCombat.plan.block, Answers: append([]int(nil), st.answers...)}
			res.Intent = answerIntent(root.Decision, rootCombat.plan.botAnswer(k, root.Bot))
		}
	} else {
		cands, kind, why, ok, cut, botFound = rootCands(root.Observer, root, opts.Kinds, opts.Limit, opts.AutoPayment, &enumBoard)
	}
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
		} else if res.Stats.countExtraSkipped(kind) {
			res.Stats.Skipped = 1
		}
		return res, nil
	}
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
	priorNet := net
	if opts.UniformPrior {
		priorNet = nil
	}
	var prior []float64
	var fell bool
	if rootCombat != nil {
		// A creature step's prior is uniform (combat.go).
		prior = uniform(len(cands))
	} else {
		prior, fell = priors(priorNet, root.Engine, root.Decision, root.Bot, kind, cands, &views.prior)
	}
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
	if opts.OpponentNodes {
		if err := checkOpponentSource(src); err != nil {
			return res, err
		}
	}
	if opts.CombatSteps && !isFixed(src) {
		return res, errors.New("azmcts: per-creature combat needs a fixed world source (the clairvoyant clone or FixedChance): a step's creature list is one world's")
	}
	if opts.ReuseTree && !isReal(src) {
		return res, errors.New("azmcts: tree reuse needs the real world source (the clairvoyant clone, a RealWorldSource); a redeal, IS-MCTS, PIMC or re-seeded chance world need not be the position the real game reaches")
	}
	res.Stats.Searched = 1
	res.Stats.countSearched(kind)
	if rootCombat != nil {
		res.Stats.CombatSteps = 1
	}
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	treePrior := prior
	if opts.Noise {
		treePrior = noisyPrior(prior, rng, opts.DirichletAlpha, opts.DirichletEps)
	}
	rootPt := &Point{Keys: res.Keys, Prior: treePrior, opp: opts.swapFrame}
	cfg := &walkConfig{
		net: net, heuristicLeaf: opts.HeuristicLeaf, leaf: opts.Leaf, kinds: opts.Kinds, limit: opts.Limit, maxSteps: opts.MaxSteps,
		envSeed: splitmix(opts.Seed ^ 0x656e762d73656564), actor: root.Decision.Player, autoPayment: opts.AutoPayment,
		uniformPrior: opts.UniformPrior, rootPerWorld: opts.RootPerWorld,
		nameKeys: opts.NameKeys, rootRefs: root.Observer.Introduced(),
		root: rootPt, rootCands: cands, rootDec: root.Decision, stats: &res.Stats,
		envBoard: &envBoard, enumBoard: &enumBoard, views: views,
		combatSteps: opts.CombatSteps, rootCombat: rootCombat,
	}
	if opts.OpponentNodes {
		if err := prepareOpponent(cfg, root, opts); err != nil {
			return res, err
		}
	}
	var envs EnvSource = &worldEnvs{src: src, cfg: cfg}
	if opts.NodeCache > 0 && isFixed(src) {
		envs = &fixedEnvs{worldEnvs: envs.(*worldEnvs)}
	}
	if searchCfgHook != nil {
		searchCfgHook(cfg)
	}
	var top *node
	carried := 0
	if opts.ReuseTree {
		top, carried = root.Reuse.adopt(root.Engine, cands, rootPt, cfg, &res.Stats)
		need := opts.Sims - carried
		if opts.ParentVisits && carried > 0 {
			// Upstream's budget counts the carried root's own expansion
			// visit too (root.getVisits() >= searchBudget; upstreamVisits
			// of a carried root is 1 + carried).
			need = opts.Sims - upstreamVisits(top)
		}
		top = runTreeFrom(ctx, top, rootPt, envs, opts, max(0, need), &res.Stats)
	} else {
		var err error
		if top, err = runTree(ctx, rootPt, envs, opts, &res.Stats); err != nil {
			return res, err
		}
	}
	if searchTreeHook != nil {
		searchTreeHook(top)
	}
	tr := treeResult(rootPt, top)
	if res.Stats.DeadlineHits > 0 {
		// The armed bail-out fired (ctx done at the call or between
		// simulations): the tree stopped where it was and the bot's answer
		// is played -- the partial tree's choice is never taken -- unless
		// Options.DeadlineBestChild asks for upstream's best child so far
		// and the tree has a visited root child to choose from.
		if !opts.DeadlineBestChild || !anyVisit(tr.Visits) {
			return res, nil
		}
		res.Stats.DeadlineBest = 1
	}
	res.Visits, res.Q, res.Avail, res.RootValue = tr.Visits, tr.Q, tr.Avail, rootValue(top, opts)
	if res.Stats.Completed == 0 && carried == 0 {
		res.Stats.AllFailed = 1
		return res, nil
	}
	res.Choice = choose(res.Visits, opts.Sample, rng)
	if res.Choice != 0 || !botFound {
		res.Intent = cands[res.Choice].in
	}
	if opts.ReuseTree {
		root.Reuse.keep(top, res.Choice, cfg)
	}
	return res, nil
}

// combatRoot is a split declaration's root (Options.CombatSteps): creature
// len(root.Combat)'s step candidates, in upstream's order, and the walk
// state every world starts from. A declaration that asks about no creature,
// or cannot be observed, is not searched (the bot's declaration is played);
// a Combat prefix the declaration does not have is an error.
func combatRoot(root Root, opts Options) (cands []cand, kind string, why SkipReason, ok, cut bool, cw *combatWalk, err error) {
	d := root.Decision
	kind = "attackers"
	if d.Kind == decision.KBlockers {
		kind = "blockers"
	}
	plan, perr := newCombatPlan(root.Observer, root.Engine, d, opts.Limit)
	switch {
	case perr != nil:
		return nil, kind, SkipTranslate, false, false, nil, nil
	case plan == nil:
		if len(root.Combat) > 0 {
			return nil, kind, 0, false, false, nil, fmt.Errorf("azmcts: Root.Combat has %d answers, the declaration asks about no creature", len(root.Combat))
		}
		return nil, kind, SkipFewCandidates, false, false, nil, nil
	}
	k := len(root.Combat)
	if k >= len(plan.steps) {
		return nil, kind, 0, false, false, nil, fmt.Errorf("azmcts: Root.Combat has %d answers, the declaration asks about %d creatures", k, len(plan.steps))
	}
	for i, a := range root.Combat {
		found := false
		for _, x := range plan.steps[i].answers {
			found = found || x == a
		}
		if !found {
			return nil, kind, 0, false, false, nil, fmt.Errorf("azmcts: Root.Combat[%d] = %d is not an answer of that creature's step", i, a)
		}
	}
	cw = &combatWalk{plan: plan, answers: append([]int(nil), root.Combat...), opp: opts.swapFrame}
	return plan.stepCands(k), kind, 0, true, plan.steps[k].cut, cw, nil
}

// anyVisit reports whether some root child holds a visit.
func anyVisit(visits []int) bool {
	for _, v := range visits {
		if v > 0 {
			return true
		}
	}
	return false
}

// searchTreeHook, when set, sees every Search's whole tree once it is built
// (the opponent-node tests read below the root). Nil outside tests.
var searchTreeHook func(top *node)

// searchCfgHook, when set, sees every Search's walk configuration before
// the tree runs (the reuse tests drive the real engine with it). Nil
// outside tests.
var searchCfgHook func(cfg *walkConfig)

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
