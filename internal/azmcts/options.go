package azmcts

import (
	"errors"
	"fmt"
	"strings"
)

// Kinds are the searched decision kinds (spec §1). Every other decision --
// and a decision of a searched kind whose candidates cannot be built -- is
// answered by the bot.
type Kinds struct {
	Priority, Attackers, Blockers, Target bool
}

// AllKinds is the spec's searched set.
func AllKinds() Kinds { return Kinds{Priority: true, Attackers: true, Blockers: true, Target: true} }

// ParseKinds parses a comma list of priority, attackers, blockers, target
// (botbench's -az-kinds). An unknown or empty entry is an error.
func ParseKinds(s string) (Kinds, error) {
	var k Kinds
	for _, part := range strings.Split(s, ",") {
		switch p := strings.TrimSpace(part); p {
		case "priority":
			k.Priority = true
		case "attackers":
			k.Attackers = true
		case "blockers":
			k.Blockers = true
		case "target":
			k.Target = true
		default:
			return Kinds{}, fmt.Errorf("unknown searched kind %q (want priority, attackers, blockers, target)", p)
		}
	}
	return k, nil
}

// Options are one Search's knobs; DefaultOptions holds the spec's values.
type Options struct {
	// Sims is the simulation budget per searched decision (-az-sims). 0 or
	// less never builds a tree: the bot's answer is played.
	Sims int
	// CPUCT is PUCT's exploration constant c (spec §2: 1.5).
	CPUCT float64
	// FPU is the first-play-urgency reduction: an unvisited child's Q is its
	// parent's Q minus FPU (spec §2: 0.1).
	FPU float64
	// Limit caps the candidates per searched decision, the bot's answer
	// first (the enumerators' limit argument).
	Limit int
	// MaxSteps caps the environment's submits per simulation, the livelock
	// guard of spec §2; a walk that reaches it is evaluated where it stopped.
	MaxSteps int
	// Kinds are the searched decision kinds.
	Kinds Kinds
	// AutoPayment lifts engine-provided automatic payment witnesses into
	// priority candidates. It is off by default so ordinary seats retain the
	// established manual-payment candidate vocabulary.
	AutoPayment bool
	// Seed is the per-decision seed (DecisionSeed): it seeds the root noise,
	// the move sampling, and -- identically for every simulation -- the
	// environment bots' streams.
	Seed uint64
	// Noise mixes Dirichlet(DirichletAlpha) noise into the root prior with
	// weight DirichletEps. Generation only (spec §2); eval leaves it false.
	Noise                        bool
	DirichletAlpha, DirichletEps float64
	// Sample picks the move with probability proportional to its root
	// visits (tau = 1) instead of the argmax. Generation only, turns 1-4
	// (the seat decides); eval leaves it false.
	Sample bool
	// HeuristicLeaf keeps the frozen heuristic leaf (searchprobe.LeafValue)
	// even when a network supplies the prior: the network's value head is
	// then unused (M1b's prior-only ablation).
	HeuristicLeaf bool
	// UniformPrior keeps the uniform prior even when a network is given: the
	// network then supplies only the leaf value (the search benchmark's
	// "priors off", upstream experiment #2's setting).
	UniformPrior bool

	// AbsoluteUnvisitedQ replaces first-play urgency: an unvisited child's Q
	// is the constant UnvisitedQ instead of its parent's Q minus FPU. The
	// search benchmark sets UnvisitedQ 0.5 with CPUCT 0.5, the [0,1]
	// equivalent of upstream MageZero's c = 1 with unvisited children valued
	// 0 on [-1,1] (values map by v01 = (v+1)/2, which halves Q's scale, so
	// c halves with it).
	AbsoluteUnvisitedQ bool
	UnvisitedQ         float64

	// Discount is the backup discount gamma; 0 and 1 are both off (the
	// default). A simulation's leaf value v reaches a node Delta units above
	// the leaf as 0.5 + (v - 0.5) * gamma^Delta: discounting shrinks the
	// value toward 0.5, "unknown", exactly as upstream's multiplication of a
	// [-1,1] value shrinks it toward 0. DiscountUnit says what Delta counts.
	Discount     float64
	DiscountUnit DiscountUnit
	// RootPerWorld makes every simulation re-derive the root candidates in
	// its own world: each root key is matched against that world's decision
	// (IntentForKey), a key the world does not offer is unavailable there
	// (the availability rule, at the root too), and the world's pending
	// decision need only share the root's kind and player, not its
	// sequence number. A multi-world source whose worlds were built
	// independently (the benchmark's IS-MCTS) needs it: a root intent's
	// option indices and payment witness are only valid on the engine that
	// built them. Every world's Observer must then be a collector that
	// captured THAT world's root.
	RootPerWorld bool
	// NameKeys names, in the key of an in-walk candidate, every object the
	// root observation did not show (a card drawn or a permanent that
	// entered during the walk) by its card name instead of its
	// observer-local reference. References are assigned in introduction
	// order, so across re-dealt worlds "the first card drawn" would share a
	// key whatever card it is; a name is what upstream's keys use (an
	// ability's text and its source's name). Two same-named new objects
	// then share one key, and one candidate is kept. Root keys are
	// unchanged. A face-down object the searching seat does not control is
	// named "face-down".
	NameKeys bool
	// NodeCache caps the tree nodes whose engine state the search stores
	// (nodecache.go) when the world source declares a fixed world
	// (FixedWorldSource: the clairvoyant clone, FixedChance); 0 turns the
	// cache off. It never changes the Result, only its cost counters, and a
	// source whose worlds differ between simulations never uses it.
	NodeCache int
}

// DiscountUnit is what one step of the backup discount counts.
type DiscountUnit uint8

const (
	// DiscountPly counts engine decisions on the simulation's path from the
	// node to the leaf: every submit, the searched intents and the
	// environment's (bot and opponent) answers alike.
	DiscountPly DiscountUnit = iota
	// DiscountAction counts searched tree edges between the node and the
	// leaf (upstream's "logical action": only edges out of the searching
	// seat's searched decisions).
	DiscountAction
	// DiscountTurn counts turn boundaries crossed between the node and the
	// leaf.
	DiscountTurn
)

// DiscountUnitNames are the units' names in constant order.
var DiscountUnitNames = [...]string{"ply", "action", "turn"}

func (u DiscountUnit) String() string {
	if int(u) < len(DiscountUnitNames) {
		return DiscountUnitNames[u]
	}
	return fmt.Sprintf("DiscountUnit(%d)", u)
}

// ParseDiscountUnit parses ply, action or turn.
func ParseDiscountUnit(s string) (DiscountUnit, error) {
	for i, n := range DiscountUnitNames {
		if n == s {
			return DiscountUnit(i), nil
		}
	}
	return 0, fmt.Errorf("unknown discount unit %q (want ply, action, turn)", s)
}

// BenchCandidateLimit is a candidate limit no benchmark decision reaches:
// the search benchmark must never cut a legal root option (Stats.Truncated
// and RootTruncated count every cut that happens anyway).
const BenchCandidateLimit = 512

// DefaultOptions are the spec's values (§2) and this plan's candidate and
// step caps.
func DefaultOptions() Options {
	return Options{
		Sims: 100, CPUCT: 1.5, FPU: 0.1, Limit: 6, MaxSteps: 1000, Kinds: AllKinds(),
		DirichletAlpha: 0.3, DirichletEps: 0.25, NodeCache: DefaultNodeCache,
	}
}

// Stats are spec §4's failure-handling counters plus the work counts the
// cost report needs. Every fallback lands here; none is silent.
type Stats struct {
	Searched       int // decisions a tree was built for
	Skipped        int // searched-kind decisions with fewer than two candidates, or a bot answer outside the candidate vocabulary: the bot's answer was played
	Simulations    int // simulations attempted
	Completed      int // simulations that backed up a value
	ChanceFailures int // chance failure in a hypothetical world: simulation discarded
	Panics         int // engine panic inside a world (the livelock watcher included): discarded
	SubmitErrors   int // any other world error (a rejected intent, no pending decision, an unknown key): discarded
	BadWorlds      int // a world not positioned at the root decision: discarded
	NoWorld        int // the source could not produce a world: discarded
	AllFailed      int // every simulation failed: the bot's answer was played
	StepCapped     int // walks stopped by MaxSteps, evaluated where they stopped
	Terminal       int // walks that reached game over
	Expanded       int // new tree nodes
	Unavailable    int // known children a world did not offer, summed over node visits
	EnvSteps       int // environment submits (the searched intents excluded), a discarded simulation's included
	PriorFallbacks int // network priors that fell back to uniform, at the root and at in-walk points (a discarded simulation's included)
	FeedStopped    int // decisions the driver routed around the search (its observation feed stopped): the bot's answer was played
	RedealRefused  int // decisions whose honest (redeal) world source refused to prepare: no world, the bot's answer was played
	// Truncated counts searched points -- the root and in-walk decisions,
	// a discarded simulation's included -- whose candidate list the Limit
	// cut; RootTruncated is the root's share (0 or 1 per Search).
	Truncated     int
	RootTruncated int
	// LeafPlies, LeafEdges and LeafTurns sum, over completed simulations,
	// the leaf's depth below the root: engine decisions (every submit, the
	// environment's included), searched tree edges, and turn boundaries
	// crossed. MeanLeafPlies and friends divide by Completed.
	LeafPlies int
	LeafEdges int
	LeafTurns int
	// DeadlineHits is the armed wall-clock bail-out (Search's ctx, the hosted
	// seats' per-decision budget): the caller's context was done at the call
	// or became done between simulations, the tree stopped where it was and
	// the bot's answer was played. A live context never counts it.
	DeadlineHits int

	// The walk's cost counters. They count work, not outcomes: the node
	// cache (Options.NodeCache) changes them, and EnvSteps above, and
	// nothing else in Stats (a stored point records the Truncated and
	// PriorFallbacks producing it counted, so those stay exact).
	Plays       int // searched submits (Env.Play), a discarded simulation's included
	ReplayPlays int // Plays along an edge the tree already held (expanded, or ended there before)
	ReplaySteps int // the EnvSteps spent inside those ReplayPlays: re-walking known tree edges
	NodeSaves   int // node states the cache stored
	NodeResumes int // simulations that resumed from a stored node state below the root
	NodeEvicts  int // stored node states dropped for a more-visited node (the cache was full)

	// KindSearched splits Searched by kind (KindNames order).
	KindSearched [NumKinds]int
	// KindSkipped splits Skipped by kind and reason: every skip is counted
	// in exactly one cell.
	KindSkipped [NumKinds][NumSkipReasons]int
	// PrioritySkipped splits the skipped priority decisions (the
	// KindSkipped[KindPriority] row) by the bot's own answer. The manual
	// bot taps mana ("activate") one source at a time before it casts, and
	// those decisions are never searched, so this row is where stage 0 sees
	// how often the search meets a cast only after mana has floated.
	PrioritySkipped [NumBaseKinds][NumSkipReasons]int
}

// The searched kinds, indexing Stats' per-kind breakdowns.
const (
	KindPriority = iota
	KindAttackers
	KindBlockers
	KindTarget
	NumKinds
)

// KindNames are the searched kinds' names in index order (the Result.Kind
// and Diag.Kind strings).
var KindNames = [NumKinds]string{"priority", "attackers", "blockers", "target"}

// kindIndex is kind's index in KindNames, or -1.
func kindIndex(kind string) int {
	for i, n := range KindNames {
		if n == kind {
			return i
		}
	}
	return -1
}

// SkipReason is why a decision of a searched kind was not searched.
type SkipReason int

const (
	// SkipPayment: the bot answered with an auto-pay Payment intent, which
	// has no semantic-action form (no Payment support yet).
	SkipPayment SkipReason = iota
	// SkipFewCandidates: fewer than two candidates -- a pass-only priority,
	// no legal attacker, or a bot answer outside the candidate vocabulary
	// (a land play or mana activation at priority).
	SkipFewCandidates
	// SkipTranslate: ObserveDecision, Actions or Match failed on the
	// decision or a candidate.
	SkipTranslate
	NumSkipReasons
)

// SkipReasonNames are the skip reasons' report names in index order.
var SkipReasonNames = [NumSkipReasons]string{"payment", "few-candidates", "translate-error"}

// BaseKind is the kind of the bot's own answer at a priority decision.
type BaseKind int

const (
	BaseCast BaseKind = iota
	BaseAbility
	BasePass
	BasePlayLand
	BaseActivate // a mana ability: the manual bot's tap before a cast
	BasePayment  // an auto-pay payment witness (Intent.Payment)
	BaseOther    // any other option kind, or not exactly one choice
	NumBaseKinds
)

// BaseKindNames are the base kinds' report names in index order.
var BaseKindNames = [NumBaseKinds]string{"cast", "ability", "pass", "play_land", "activate", "payment", "other"}

// Add sums o into s.
func (s *Stats) Add(o Stats) {
	s.Searched += o.Searched
	s.Skipped += o.Skipped
	s.Simulations += o.Simulations
	s.Completed += o.Completed
	s.ChanceFailures += o.ChanceFailures
	s.Panics += o.Panics
	s.SubmitErrors += o.SubmitErrors
	s.BadWorlds += o.BadWorlds
	s.NoWorld += o.NoWorld
	s.AllFailed += o.AllFailed
	s.StepCapped += o.StepCapped
	s.Terminal += o.Terminal
	s.Expanded += o.Expanded
	s.Unavailable += o.Unavailable
	s.EnvSteps += o.EnvSteps
	s.PriorFallbacks += o.PriorFallbacks
	s.FeedStopped += o.FeedStopped
	s.RedealRefused += o.RedealRefused
	s.DeadlineHits += o.DeadlineHits
	s.Truncated += o.Truncated
	s.RootTruncated += o.RootTruncated
	s.LeafPlies += o.LeafPlies
	s.LeafEdges += o.LeafEdges
	s.LeafTurns += o.LeafTurns
	s.Plays += o.Plays
	s.ReplayPlays += o.ReplayPlays
	s.ReplaySteps += o.ReplaySteps
	s.NodeSaves += o.NodeSaves
	s.NodeResumes += o.NodeResumes
	s.NodeEvicts += o.NodeEvicts
	for k := range s.KindSearched {
		s.KindSearched[k] += o.KindSearched[k]
		for r := range s.KindSkipped[k] {
			s.KindSkipped[k][r] += o.KindSkipped[k][r]
		}
	}
	for b := range s.PrioritySkipped {
		for r := range s.PrioritySkipped[b] {
			s.PrioritySkipped[b][r] += o.PrioritySkipped[b][r]
		}
	}
}

// MeanLeafPlies is LeafPlies per completed simulation (0 with none).
func (s Stats) MeanLeafPlies() float64 { return perCompleted(s.LeafPlies, s.Completed) }

// MeanLeafEdges is LeafEdges per completed simulation (0 with none).
func (s Stats) MeanLeafEdges() float64 { return perCompleted(s.LeafEdges, s.Completed) }

// MeanLeafTurns is LeafTurns per completed simulation (0 with none).
func (s Stats) MeanLeafTurns() float64 { return perCompleted(s.LeafTurns, s.Completed) }

func perCompleted(sum, n int) float64 {
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

// The error classes a world reports; RunTree counts a discarded simulation
// by the first class its error wraps (anything else is a SubmitError).
var (
	ErrChance   = errors.New("azmcts: chance failure in a hypothetical world")
	ErrPanic    = errors.New("azmcts: engine panic inside a world")
	ErrSubmit   = errors.New("azmcts: a world rejected a submit")
	ErrBadWorld = errors.New("azmcts: world is not at the root decision")
	ErrNoWorld  = errors.New("azmcts: no world could be produced")
)
