package searchbench

// The search benchmark's four methods (docs/016 §1.2; upstream
// java/mzbridge BenchSearch.java) and the no-search baseline, over engines
// already positioned at the item's root decision.
//
// Every arm is one call, RunArm, a pure function of its input: no clock, no
// ambient randomness, no map range that reaches a choice. Timing belongs to
// the command boundary.
//
// Stated deviations from BenchSearch (the tree, PUCT, leaf and backup
// semantics are azmcts's; docs/superpowers/plans/2026-09-30-searchbench-
// replication.md "Search semantics" lists the rest):
//
//   - Upstream caches each tree node's engine state the first time it is
//     visited, so a node's chance outcome is drawn once. azmcts replays every
//     simulation from the root instead, so the tree arms (clairvoyant,
//     PIMC-k) fix each tree's future chance for the whole search: every
//     simulation clones the world with the SAME chance seed (the
//     clairvoyant arm keeps the real engine's own generator; a PIMC tree is
//     re-seeded once from the search's stream, never the world's). Every
//     path is then deterministic, so a node has one outcome, as upstream's
//     cached node does. ArmInput.FreshChance re-seeds every simulation
//     instead (open loop), for comparison.
//   - IS-MCTS re-deals every simulation and re-seeds its chance, as
//     upstream's shadowRoot does for every iteration.
//   - The opponent and every unsearched decision are answered by botpolicy
//     (upstream's tree holds both players' decisions); the searching seat's
//     own unsearched decisions are answered by the auto-pay bot.
//   - A world that refuses a re-deal, or a simulation that fails, is
//     discarded and counted (azmcts.Stats); upstream retries once without
//     the re-deal.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// SearchArm names one benchmark method.
type SearchArm string

const (
	// ArmClairvoyant is one tree on the real world: every simulation walks a
	// clone of ArmInput.Real, hidden zones included. It is refused unless
	// the driving command called clairvoyant.AllowClairvoyant.
	ArmClairvoyant SearchArm = "clairvoyant-mcts"
	// ArmPIMC1 is one tree on Worlds[0].
	ArmPIMC1 SearchArm = "pimc-1"
	// ArmPIMC4 is four independent trees on Worlds[0..3], the budget split
	// evenly (the remainder to the first trees), root children merged by
	// semantic key, the choice by summed visits.
	ArmPIMC4 SearchArm = "pimc-4"
	// ArmISMCTS is one availability-count tree over all the worlds: every
	// simulation picks a world with the search's own stream and re-deals
	// its hidden cards.
	ArmISMCTS SearchArm = "is-mcts"
	// ArmNoSearch plays the redacted auto-pay bot's answer: the stand-in for
	// upstream's "rule heuristic" baseline.
	ArmNoSearch SearchArm = "no-search"
)

// Arms lists every arm in report order.
var Arms = []SearchArm{ArmNoSearch, ArmClairvoyant, ArmPIMC1, ArmPIMC4, ArmISMCTS}

// ParseArm parses an arm name.
func ParseArm(s string) (SearchArm, error) {
	for _, a := range Arms {
		if string(a) == s {
			return a, nil
		}
	}
	return "", fmt.Errorf("searchbench: unknown search arm %q", s)
}

// PIMCWorlds is PIMC-4's tree count.
const PIMCWorlds = 4

// BenchOptions are the benchmark's azmcts knobs: c = 0.5 with unvisited
// children valued 0.5 (upstream's c = 1, unvisited 0, on [-1,1]), uniform
// priors, auto-pay priority candidates, no candidate cap in practice,
// in-walk keys that name newly seen cards (upstream's name keys), no root
// noise, the final choice by visits, and no discount (gamma = 1).
func BenchOptions(sims int) azmcts.Options {
	o := azmcts.DefaultOptions()
	o.Sims = sims
	o.CPUCT = 0.5
	o.AbsoluteUnvisitedQ, o.UnvisitedQ = true, 0.5
	o.Limit = azmcts.BenchCandidateLimit
	o.AutoPayment = true
	o.UniformPrior = true
	o.NameKeys = true
	o.Noise, o.Sample = false, false
	return o
}

// ArmInput is one arm's search of one item.
type ArmInput struct {
	Arm SearchArm
	// Options are the tree's knobs (BenchOptions). Options.Seed is ignored:
	// every seed derives from Seed.
	Options azmcts.Options
	// Seed is the search seed: the bot's answer, each tree's seed and chance,
	// and IS-MCTS's world and re-deal stream.
	Seed uint64
	// Real is the item's position (StateSpec "real"), its engine at the root
	// decision. The clairvoyant arm searches it; every other arm reads only
	// its root decision (to play the chosen key on it), never its hidden
	// zones. May be nil for the honest arms, which then answer on Worlds[0].
	Real *rules.Engine
	// Worlds are the belief worlds (StateSpec "worlds"), each at the same
	// root decision with the same observation. PIMC-1 reads Worlds[0],
	// PIMC-4 Worlds[0..3], IS-MCTS all of them.
	Worlds []*rules.Engine
	// Net, when non-nil, is the leaf value network (a policynet value
	// checkpoint on the redacted view); nil is the heuristic leaf.
	Net *policynet.Model
	// NetPrior also uses Net's policy as the PUCT prior. Off, priors are
	// uniform whatever Net is.
	NetPrior bool
	// FreshChance re-seeds a tree arm's future chance for every simulation
	// (open loop) instead of once per tree. IS-MCTS always re-seeds.
	FreshChance bool
	// OnWorld, when non-nil, sees every world engine an honest arm's
	// simulation is dealt (PIMC's per-simulation clone, IS-MCTS's re-deal)
	// before the simulation walks it. It must not mutate the engine. The
	// leak probes use it to prove a card no belief can deal never reaches a
	// search; the clairvoyant arm searches Real and does not call it.
	OnWorld func(*rules.Engine)
}

// LoadLeafNet loads a policynet value checkpoint for ArmInput.Net: a
// checkpoint with a value head on the redacted features (azmcts refuses
// anything else when the arm runs).
func LoadLeafNet(path string) (*policynet.Model, error) {
	m, err := policynet.LoadCheckpointFile(path)
	if err != nil {
		return nil, err
	}
	if !m.HasValue() {
		return nil, fmt.Errorf("searchbench: checkpoint %s has no value head", path)
	}
	return m, nil
}

// ArmResult is one arm's answer.
type ArmResult struct {
	Arm  SearchArm
	Kind string // the root's searched kind, "" when not a searched kind
	// Searched is true when the answer came from a tree; false when the
	// bot's answer was played (no-search, a decision that is not searched,
	// every simulation failed).
	Searched bool
	// Intent is the answer, valid on the answer engine: Real when given,
	// else Worlds[0]. Key and Label name it ("" when not searched).
	Intent decision.Intent
	Key    azmcts.Key
	Label  string
	// Bot is the redacted auto-pay bot's answer on the answer engine.
	Bot decision.Intent
	// Choice indexes Table (-1 when not searched).
	Choice int
	// Table is the root table (merged over PIMC-4's trees).
	Table []azmcts.RootRow
	// PerWorld is PIMC-4's per-tree root tables.
	PerWorld  [][]azmcts.RootRow
	RootValue float64
	Stats     azmcts.Stats
	// IS-MCTS: re-deals made and failed, and simulations per world.
	Redeals, RedealFailures int
	WorldPicks              []int

	// plan is the full root of the first tree (planRoot), nil when the
	// root is not a priority decision.
	plan *rootPlan
}

// RootMacros and RootUnreached count the full root's macro candidates and
// the canonical plays no candidate reaches (0 without a full root).
func (r ArmResult) RootMacros() int {
	if r.plan == nil {
		return 0
	}
	return len(r.plan.Macros)
}

func (r ArmResult) RootUnreached() []string {
	if r.plan == nil {
		return nil
	}
	return r.plan.Unreached
}

// RunArm runs one arm. The error return is misconfiguration or an input
// that is not a benchmark root (worlds that disagree with the observation,
// a missing world, a refused re-deal); a search failure is counted in Stats
// and the bot's answer is played.
func RunArm(ctx context.Context, in ArmInput) (ArmResult, error) {
	res := ArmResult{Arm: in.Arm, Choice: -1}
	if ctx == nil {
		ctx = context.Background()
	}
	need := 0
	switch in.Arm {
	case ArmClairvoyant:
		if in.Real == nil {
			return res, errors.New("searchbench: clairvoyant-mcts needs the real world")
		}
	case ArmNoSearch:
		if in.Real == nil {
			need = 1
		}
	case ArmPIMC1:
		need = 1
	case ArmPIMC4:
		need = PIMCWorlds
	case ArmISMCTS:
		need = 1
	default:
		return res, fmt.Errorf("searchbench: unknown search arm %q", in.Arm)
	}
	if in.Options.OpponentNodes && (in.Arm == ArmISMCTS || in.FreshChance) {
		// An opponent node's statistics must describe one world: IS-MCTS
		// re-deals and fresh chance re-seeds every simulation.
		what := string(in.Arm)
		if in.Arm != ArmISMCTS {
			what += " with fresh chance"
		}
		return res, fmt.Errorf("searchbench: opponent nodes need a fixed-world tree; %s changes the world every simulation", what)
	}
	if len(in.Worlds) < need {
		return res, fmt.Errorf("searchbench: %s needs %d worlds, got %d", in.Arm, need, len(in.Worlds))
	}
	answer := in.Real
	if answer == nil {
		answer = in.Worlds[0]
	}
	actor, err := checkRoots(answer, in.Real, in.Worlds)
	if err != nil {
		return res, err
	}
	botSeed := splitMix64(in.Seed ^ 0x626f742d61727365)
	res.Bot = botAnswer(answer, botSeed)
	res.Intent = res.Bot
	if in.Arm == ArmNoSearch {
		return res, nil
	}
	opts := in.Options
	opts.UniformPrior = !in.NetPrior
	if err := opts.Validate(in.Net); err != nil {
		return res, err
	}
	var single azmcts.Result
	switch in.Arm {
	case ArmClairvoyant:
		obs, err := rootObserver(in.Real, actor)
		if err != nil {
			return res, err
		}
		src, err := clairvoyant.NewClairvoyant(in.Real, obs)
		if err != nil {
			return res, err
		}
		opts.Seed = treeSeed(in.Seed, 0)
		single, res.plan, err = search(ctx, in.Real, obs, botSeed, src, in.Net, opts)
		if err != nil {
			return res, err
		}
	case ArmPIMC1:
		obs, err := rootObserver(in.Worlds[0], actor)
		if err != nil {
			return res, err
		}
		opts.Seed = treeSeed(in.Seed, 0)
		src := &pimcSource{base: in.Worlds[0], obs: obs, seed: chanceSeed(in.Seed, 0), fresh: in.FreshChance, onWorld: in.OnWorld}
		single, res.plan, err = search(ctx, in.Worlds[0], obs, botSeed, src, in.Net, opts)
		if err != nil {
			return res, err
		}
	case ArmISMCTS:
		src, err := newISSource(in.Worlds, actor, splitMix64(in.Seed^0x69736d6374732d77))
		if err != nil {
			return res, err
		}
		src.onWorld = in.OnWorld
		opts.Seed = treeSeed(in.Seed, 0)
		opts.RootPerWorld = true
		obs, err := rootObserver(in.Worlds[0], actor)
		if err != nil {
			return res, err
		}
		single, res.plan, err = search(ctx, in.Worlds[0], obs, botSeed, src, in.Net, opts)
		if err != nil {
			return res, err
		}
		res.Redeals, res.RedealFailures, res.WorldPicks = src.redeals, src.failures, src.picks
	case ArmPIMC4:
		return runPIMC4(ctx, in, res, answer, actor, botSeed, opts)
	}
	res.Kind, res.Stats, res.RootValue = single.Kind, single.Stats, single.RootValue
	res.Table = single.RootTable()
	if !chosen(single) {
		return res, nil
	}
	return answerWith(res, answer, actor, single.Choice)
}

// chosen reports whether r's answer came from its tree.
func chosen(r azmcts.Result) bool {
	return r.Stats.Searched > 0 && r.Stats.Completed > 0 && r.Stats.DeadlineHits == 0 && r.Choice >= 0 && r.Choice < len(r.Keys)
}

// answerWith plays root row choice of res.Table on the answer engine.
func answerWith(res ArmResult, answer *rules.Engine, actor state.PlayerID, choice int) (ArmResult, error) {
	row := res.Table[choice]
	if azmcts.IsMacroKey(row.Key) {
		// A macro's answer on the answer engine is its first step (every
		// world shares the root observation, so it is offered there too).
		if res.plan != nil {
			for _, m := range res.plan.Macros {
				if m.Key == row.Key {
					in, err := m.Steps[0].Intent(answer, answer.Pending())
					if err != nil {
						return res, fmt.Errorf("searchbench: the chosen %s cannot be played on the answer engine: %w", row.Label, err)
					}
					res.Searched, res.Choice, res.Key, res.Label, res.Intent = true, choice, row.Key, row.Label, in
					return res, nil
				}
			}
		}
		return res, fmt.Errorf("searchbench: the chosen macro %s is not in the root plan", row.Label)
	}
	obs, err := rootObserver(answer, actor)
	if err != nil {
		return res, err
	}
	intent, err := azmcts.IntentForKey(obs, answer, answer.Pending(), row.Key)
	if err != nil {
		return res, fmt.Errorf("searchbench: the chosen %s cannot be played on the answer engine: %w", row.Label, err)
	}
	res.Searched, res.Choice, res.Key, res.Label, res.Intent = true, choice, row.Key, row.Label, intent
	return res, nil
}

// rootObserver is a private collector for actor that has captured e at its
// root. Every tree's root observer and every world's observer is one, so a
// key's references mean the same objects on every engine whose
// observation matches (checkRoots): references follow the capture's
// order, not the order a decision happened to introduce its objects.
func rootObserver(e *rules.Engine, actor state.PlayerID) (*searchprobe.Collector, error) {
	obs := searchprobe.NewCollector(actor)
	if _, err := obs.Capture(e, nil); err != nil {
		return nil, fmt.Errorf("searchbench: capture the root: %w", err)
	}
	return obs, nil
}

// search is one azmcts.Search rooted at e. A priority root under
// auto-payment is the full root (planRoot): every canonical play is a
// candidate, and the bot's candidate is never a mana tap.
func search(ctx context.Context, e *rules.Engine, obs *searchprobe.Collector, botSeed uint64, src azmcts.WorldSource, net *policynet.Model, opts azmcts.Options) (azmcts.Result, *rootPlan, error) {
	d := e.Pending()
	root := azmcts.Root{Engine: e, Decision: d, Bot: botAnswer(e, botSeed), Observer: obs}
	var plan *rootPlan
	if d.Kind == decision.KPriority && opts.AutoPayment {
		p := planRoot(e, root.Bot, botSeed)
		plan = &p
		root.Bot, root.BotKey, root.Macros, root.NoBot = p.Bot, p.BotKey, p.Macros, true
	}
	r, err := azmcts.Search(ctx, root, src, net, opts)
	return r, plan, err
}

// botAnswer is the redacted auto-pay bot's answer at e's pending decision:
// seat.Bot on the searching seat's projected view, payment actions built.
func botAnswer(e *rules.Engine, seed uint64) decision.Intent {
	d := e.Pending()
	if d.Kind == decision.KPriority {
		e.EnsurePaymentActions()
	}
	in, _ := seat.NewBot(seed).EnableAutoPayMana().Decide(context.Background(), view.Project(e.G, e, d.Player, d), *d)
	return in
}

// checkRoots checks every engine is at a pending decision of one player
// and that each observes exactly what the answer engine does (the board,
// every identity, the decision): a world that disagrees with the
// observation is not a belief world of this item, and a key would name
// different cards in it.
func checkRoots(answer, real *rules.Engine, worlds []*rules.Engine) (state.PlayerID, error) {
	d := answer.Pending()
	if answer.G.Over || d == nil {
		return 0, errors.New("searchbench: the answer engine is not at a decision")
	}
	actor := d.Player
	want, err := searchprobe.NewCollector(actor).Capture(answer, nil)
	if err != nil {
		return 0, fmt.Errorf("searchbench: capture the root: %w", err)
	}
	check := func(name string, e *rules.Engine) error {
		if e == nil {
			return fmt.Errorf("searchbench: %s is nil", name)
		}
		pd := e.Pending()
		if e.G.Over || pd == nil || pd.Player != actor || pd.Kind != d.Kind {
			return fmt.Errorf("searchbench: %s is not at the root decision", name)
		}
		got, err := searchprobe.NewCollector(actor).Capture(e, nil)
		if err != nil {
			return fmt.Errorf("searchbench: capture %s: %w", name, err)
		}
		switch {
		case got.Board.Sum != want.Board.Sum: // equal exactly when the boards' JSON encodings are
			return fmt.Errorf("searchbench: %s observes a different board", name)
		case !searchprobe.IdentitiesEqual(got.Identities, want.Identities):
			return fmt.Errorf("searchbench: %s observes different identities", name)
		case !searchprobe.ObservedDecisionEqual(got.Decision, want.Decision):
			return fmt.Errorf("searchbench: %s observes a different decision", name)
		}
		return nil
	}
	if real != nil && real != answer {
		if err := check("the real world", real); err != nil {
			return 0, err
		}
	}
	for i, w := range worlds {
		if w == answer {
			continue
		}
		if err := check(fmt.Sprintf("world %d", i), w); err != nil {
			return 0, err
		}
	}
	return actor, nil
}

// runPIMC4 is four independent trees, one per world, merged by key.
func runPIMC4(ctx context.Context, in ArmInput, res ArmResult, answer *rules.Engine, actor state.PlayerID, botSeed uint64, opts azmcts.Options) (ArmResult, error) {
	results := make([]azmcts.Result, PIMCWorlds)
	for i := 0; i < PIMCWorlds; i++ {
		local := opts
		local.Sims = opts.Sims / PIMCWorlds
		if i < opts.Sims%PIMCWorlds {
			local.Sims++
		}
		local.Seed = treeSeed(in.Seed, i)
		obs, err := rootObserver(in.Worlds[i], actor)
		if err != nil {
			return res, err
		}
		src := &pimcSource{base: in.Worlds[i], obs: obs, seed: chanceSeed(in.Seed, i), fresh: in.FreshChance, onWorld: in.OnWorld}
		r, plan, err := search(ctx, in.Worlds[i], obs, botSeed, src, in.Net, local)
		if err != nil {
			return res, fmt.Errorf("searchbench: pimc-4 world %d: %w", i, err)
		}
		if res.plan == nil {
			res.plan = plan
		}
		results[i] = r
		res.Stats.Add(r.Stats)
		res.PerWorld = append(res.PerWorld, r.RootTable())
		if res.Kind == "" {
			res.Kind = r.Kind
		}
	}
	res.Table = MergeRootTables(res.PerWorld)
	rootValue, rootVisits := 0.0, 0
	for _, r := range results {
		n := 0
		for _, v := range r.Visits {
			n += v
		}
		rootValue += r.RootValue * float64(n)
		rootVisits += n
	}
	if rootVisits > 0 {
		res.RootValue = rootValue / float64(rootVisits)
	}
	best := -1
	for i, row := range res.Table {
		if row.Visits > 0 && (best < 0 || row.Visits > res.Table[best].Visits) {
			best = i
		}
	}
	if best < 0 {
		return res, nil
	}
	return answerWith(res, answer, actor, best)
}

// MergeRootTables merges independent trees' root tables by key (PIMC's
// "merge the root by label"): the union of keys in order of first
// appearance, visits and availability summed, Q visit-weighted, the prior
// averaged over the tables that hold the key, the first label kept. A key
// one world does not offer is simply absent from that world's table.
func MergeRootTables(tables [][]azmcts.RootRow) []azmcts.RootRow {
	var out []azmcts.RootRow
	var qw []float64
	var seen []int
	at := make(map[azmcts.Key]int) // lookup only -- never ranged.
	for _, t := range tables {
		for _, row := range t {
			i, ok := at[row.Key]
			if !ok {
				i = len(out)
				at[row.Key] = i
				out = append(out, azmcts.RootRow{Key: row.Key, Label: row.Label})
				qw, seen = append(qw, 0), append(seen, 0)
			}
			out[i].Visits += row.Visits
			out[i].Avail += row.Avail
			out[i].Prior += row.Prior
			qw[i] += row.Q * float64(row.Visits)
			seen[i]++
		}
	}
	for i := range out {
		if out[i].Visits > 0 {
			out[i].Q = qw[i] / float64(out[i].Visits)
		}
		out[i].Prior /= float64(seen[i])
	}
	return out
}

// pimcSource is a tree arm's world: a hypothetical clone of base whose
// future chance is seed -- the same for every simulation (one chance
// outcome per node, as upstream's cached nodes), or with fresh a new seed
// per simulation.
type pimcSource struct {
	base  *rules.Engine
	obs   *searchprobe.Collector
	seed  uint64
	fresh bool
	prev  *rules.Engine
	spare rules.Spare

	onWorld func(*rules.Engine)
}

func (s *pimcSource) World(sim int) (azmcts.World, error) {
	if s.prev != nil {
		s.spare = s.prev.Release()
	}
	seed := s.seed
	if s.fresh {
		seed = splitMix64(s.seed ^ splitMix64(uint64(sim)+1))
	}
	s.prev = s.base.CloneHypotheticalInto(seed, &s.spare)
	if s.onWorld != nil {
		s.onWorld(s.prev)
	}
	return azmcts.World{Engine: s.prev, Observer: s.obs.Clone(), Hypothetical: true}, nil
}

// FixedWorld is true unless fresh: one base and one chance seed per tree is
// one world, so the tree's node cache applies (azmcts.FixedWorldSource,
// exactly as azmcts.FixedChance). A fresh seed per simulation is not.
func (s *pimcSource) FixedWorld() bool { return !s.fresh }

// isSource is IS-MCTS's world source. Simulation sim draws, from its own
// PCG stream (the search's, seeded by (seed, sim)), a world index i and a
// deal seed, and re-deals world i: every card its searching seat cannot see
// -- the opponent's hand and library, the seat's own library order -- is
// dealt uniformly from world i's unseen cards, with fresh future chance
// (searchprobe.Redealer over OneFrameRedealInput). Each world's observer is
// the collector that captured its root, so RootPerWorld matches the root
// keys against its own objects.
type isSource struct {
	worlds    []isWorld
	seed      uint64
	picks     []int
	redeals   int
	failures  int
	firstFail string
	prev      *rules.Engine
	spare     rules.Spare
	onWorld   func(*rules.Engine)
}

type isWorld struct {
	r   *searchprobe.Redealer
	obs *searchprobe.Collector
}

func newISSource(worlds []*rules.Engine, actor state.PlayerID, seed uint64) (*isSource, error) {
	s := &isSource{seed: seed, picks: make([]int, len(worlds))}
	for i, w := range worlds {
		in, obs, err := OneFrameRedealInput(w, actor, WorldDecks(w))
		if err != nil {
			return nil, fmt.Errorf("searchbench: is-mcts world %d: %w", i, err)
		}
		r, refused := searchprobe.NewRedealer(in.Setup, in.History, in.Known, in.Base)
		if refused != "" {
			return nil, fmt.Errorf("searchbench: is-mcts world %d refuses a re-deal: %s", i, refused)
		}
		s.worlds = append(s.worlds, isWorld{r: r, obs: obs})
	}
	return s, nil
}

func (s *isSource) World(sim int) (azmcts.World, error) {
	if s.prev != nil {
		s.spare = s.prev.Release()
		s.prev = nil
	}
	seed := azmcts.RedealSeed(s.seed, sim)
	rng := rand.New(rand.NewPCG(seed[0], seed[1]))
	i := rng.IntN(len(s.worlds))
	s.picks[i]++
	w, reason := s.worlds[i].r.Deal([2]uint64{rng.Uint64(), rng.Uint64()}, &s.spare)
	if reason != "" {
		s.failures++
		if s.firstFail == "" {
			s.firstFail = reason
		}
		return azmcts.World{}, fmt.Errorf("%w: world %d re-deal: %s", azmcts.ErrNoWorld, i, reason)
	}
	s.redeals++
	s.prev = w
	if s.onWorld != nil {
		s.onWorld(w)
	}
	return azmcts.World{Engine: w, Observer: s.worlds[i].obs.Clone(), Hypothetical: true}, nil
}

// OneFrameRedealInput is the redeal of one engine positioned at a decision, for a
// seat whose only observation is that decision's frame: an engine staged
// from a StateSpec has no observation history. History is the one frame a
// fresh collector captures there; the known-card projection of one frame is
// the seat's own hand (nothing else is pinned, as upstream re-deals the
// opponent's whole hand); Setup.Decks are decks, the lists the pool is
// derived from. It returns the collector that captured the frame, which
// maps the dealt worlds' objects.
func OneFrameRedealInput(e *rules.Engine, actor state.PlayerID, decks [][]*cards.Card) (azmcts.RedealInput, *searchprobe.Collector, error) {
	obs := searchprobe.NewCollector(actor)
	frame, err := obs.Capture(e, nil)
	if err != nil {
		return azmcts.RedealInput{}, nil, err
	}
	h := searchprobe.History{Actor: actor, Frames: []searchprobe.Frame{frame}}
	known, err := searchprobe.ProjectKnownCards(h)
	if err != nil {
		return azmcts.RedealInput{}, nil, err
	}
	return azmcts.RedealInput{
		Setup:   searchprobe.PublicGame{Decks: decks},
		History: h, Known: known,
		Base: searchprobe.RedealBase{Engine: e, Observer: obs},
	}, obs, nil
}

// WorldDecks is each player's card list in a belief world: every non-token,
// non-copy card it owns in any zone. A belief world is not the real game --
// its hidden cards are the belief's sample -- so the pool a re-deal draws
// the opponent's hand from is that world's own unseen cards (upstream's
// "that world's unseen cards").
func WorldDecks(e *rules.Engine) [][]*cards.Card {
	decks := make([][]*cards.Card, len(e.G.Players))
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Card == nil || len(o.Card.Faces) == 0 || o.IsToken || o.IsCopy || int(o.Owner) >= len(decks) {
			continue
		}
		switch o.Zone {
		case state.ZHand, state.ZLibrary, state.ZBattlefield, state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand:
			decks[o.Owner] = append(decks[o.Owner], o.Card)
		}
	}
	return decks
}

// RedealWorlds deals n belief worlds from e for its pending decision's
// seat: the one-frame redeal (OneFrameRedealInput) of e over decks, world
// i dealt by azmcts.RedealSeed(seed, i). A fixture's stand-in for the
// benchmark's belief sampler: each world keeps what the seat sees and
// re-deals the rest from the pool decks leave.
func RedealWorlds(e *rules.Engine, decks [][]*cards.Card, n int, seed uint64) ([]*rules.Engine, error) {
	d := e.Pending()
	if d == nil {
		return nil, errors.New("searchbench: no pending decision")
	}
	in, _, err := OneFrameRedealInput(e, d.Player, decks)
	if err != nil {
		return nil, err
	}
	r, refused := searchprobe.NewRedealer(in.Setup, in.History, in.Known, in.Base)
	if refused != "" {
		return nil, fmt.Errorf("searchbench: redeal refused: %s", refused)
	}
	out := make([]*rules.Engine, n)
	for i := range out {
		w, reason := r.Deal(azmcts.RedealSeed(seed, i), nil)
		if reason != "" {
			return nil, fmt.Errorf("searchbench: world %d: %s", i, reason)
		}
		out[i] = w
	}
	return out, nil
}

// treeSeed and chanceSeed are tree i's search seed and chance seed.
func treeSeed(seed uint64, i int) uint64 { return splitMix64(seed ^ splitMix64(uint64(i)+0x7472)) }
func chanceSeed(seed uint64, i int) uint64 {
	return splitMix64(seed ^ 0x6368616e6365 ^ splitMix64(uint64(i)+1))
}
