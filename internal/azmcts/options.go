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
	// NodeCache caps the tree nodes whose engine state the search stores
	// (nodecache.go) when the world source declares a fixed world
	// (FixedWorldSource: the clairvoyant clone, FixedChance); 0 turns the
	// cache off. It never changes the Result, only its cost counters, and a
	// source whose worlds differ between simulations never uses it.
	NodeCache int
}

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
	// DeadlineHits is the armed wall-clock bail-out (Search's ctx, the hosted
	// seats' per-decision budget): the caller's context was done at the call
	// or became done between simulations, the tree stopped where it was and
	// the bot's answer was played. A live context never counts it.
	DeadlineHits int

	// The walk's cost counters. They count work, not outcomes: the node
	// cache (Options.NodeCache) changes them, and EnvSteps and PriorFallbacks
	// above, and nothing else in Stats.
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

// The error classes a world reports; RunTree counts a discarded simulation
// by the first class its error wraps (anything else is a SubmitError).
var (
	ErrChance   = errors.New("azmcts: chance failure in a hypothetical world")
	ErrPanic    = errors.New("azmcts: engine panic inside a world")
	ErrSubmit   = errors.New("azmcts: a world rejected a submit")
	ErrBadWorld = errors.New("azmcts: world is not at the root decision")
	ErrNoWorld  = errors.New("azmcts: no world could be produced")
)
