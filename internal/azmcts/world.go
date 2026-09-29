package azmcts

import (
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/rules"
)

// World is one simulation's world: an engine positioned at the root
// decision, and an observer (a collector for the searching seat) that knows
// the root decision's objects. Hypothetical marks an engine built by
// rules.NewHypothetical or CloneHypothetical, stepped with
// SubmitHypothetical so a chance failure is reported, not panicked.
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
