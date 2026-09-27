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
}

// DefaultOptions are the spec's values (§2) and this plan's candidate and
// step caps.
func DefaultOptions() Options {
	return Options{
		Sims: 100, CPUCT: 1.5, FPU: 0.1, Limit: 6, MaxSteps: 1000, Kinds: AllKinds(),
		DirichletAlpha: 0.3, DirichletEps: 0.25,
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
	EnvSteps       int // environment submits (the searched intents excluded)
	PriorFallbacks int // decisions whose network prior fell back to uniform
	FeedStopped    int // decisions the driver routed around the search (its observation feed stopped): the bot's answer was played
}

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
