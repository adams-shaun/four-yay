package azmcts

import (
	"fmt"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// World is one simulation's world: an engine positioned at the root
// decision, and an observer (a collector for the searching seat) that knows
// the root decision's objects. Hypothetical marks an engine built by
// rules.NewHypothetical or CloneHypothetical, stepped with
// SubmitHypothetical so a chance failure is reported, not panicked.
//
// The env switches the engine's decision arena on (rules.Engine.SetDecisionArena):
// a source may Release a world once the next simulation asks for its own,
// but must not Release it while that simulation still runs.
type World struct {
	Engine       *rules.Engine
	Observer     *searchprobe.Collector
	Hypothetical bool
}

// WorldSource hands each simulation a fresh world (spec §1). Nothing else in
// the search touches hidden information. An error wrapping ErrNoWorld means
// no world could be produced (ticket 5's sampler starving with no redeal).
// The clairvoyant source (a clone of the REAL engine) lives in
// internal/azmcts/clairvoyant and reaches the seat only through
// SeatConfig.Source; this package keeps only the honest RedealSource.
type WorldSource interface {
	World(sim int) (World, error)
}

// FixedWorldSource is a WorldSource that promises every World it hands out
// is the same position with the same future chance -- a clone of one engine
// with its generator carried (the clairvoyant source) or re-seeded to one
// seed per tree (FixedChance) -- so every simulation's walk is a function of
// the keys it plays. Search then turns on the node cache (Options.NodeCache)
// and asks the source for a world only until the root's state is saved. A
// source whose worlds differ between simulations (a redeal, a per-simulation
// chance seed) must not implement it, or return false.
type FixedWorldSource interface {
	WorldSource
	FixedWorld() bool
}

// isFixed reports whether src declares a fixed world.
func isFixed(src WorldSource) bool {
	f, ok := src.(FixedWorldSource)
	return ok && f.FixedWorld()
}

// FixedChance is a PIMC tree's world source: every simulation walks a
// hypothetical clone of one world, Base, whose future chance is re-seeded
// with the SAME Seed every time (rules.Engine.CloneHypothetical), so a
// node's chance outcome is drawn once per tree, as upstream BenchSearch's
// cached nodes draw it. It is a FixedWorldSource. Observer is the collector
// that captured Base at the root; each world gets a clone of it.
type FixedChance struct {
	Base     *rules.Engine
	Observer *searchprobe.Collector
	Seed     uint64
	prev     *rules.Engine
	spare    rules.Spare
}

func (s *FixedChance) World(int) (World, error) {
	if s.Base == nil || s.Observer == nil {
		return World{}, fmt.Errorf("%w: the fixed-chance source needs a base engine and observer", ErrBadWorld)
	}
	if s.prev != nil {
		s.spare = s.prev.Release()
	}
	s.prev = s.Base.CloneHypotheticalInto(s.Seed, &s.spare)
	return World{Engine: s.prev, Observer: s.Observer.Clone(), Hypothetical: true}, nil
}

// FixedWorld is true: one world, one chance seed.
func (s *FixedChance) FixedWorld() bool { return true }
