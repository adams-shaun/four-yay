package azmcts

import (
	"errors"
	"sync/atomic"

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
type WorldSource interface {
	World(sim int) (World, error)
}

// ErrClairvoyantRefused is NewClairvoyant's answer until the driving command
// calls AllowClairvoyant.
var ErrClairvoyantRefused = errors.New("azmcts: the clairvoyant world source clones the REAL engine -- hidden zones and future chance included -- and is refused unless the driving command called AllowClairvoyant (cmd/botbench does for a -az-world clairvoyant side; no host or gorged path does)")

var clairvoyantAllowed atomic.Bool

// AllowClairvoyant opens the clairvoyant source for this process. Only a
// bench or training command calls it, once, before any game starts (spec
// §1: stage 1 is bench and training only). host, host/httpapi and
// cmd/gorged cannot link this package at all (internal/archtest).
func AllowClairvoyant() { clairvoyantAllowed.Store(true) }

type clairvoyant struct {
	e   *rules.Engine
	obs *searchprobe.Collector
}

// NewClairvoyant is the stage-1 source: every simulation walks a Clone of
// the real engine e, with a Clone of obs. obs must be the collector Search
// is given as Root.Observer; World clones it lazily, so every clone is taken
// after Search has observed the root decision.
func NewClairvoyant(e *rules.Engine, obs *searchprobe.Collector) (WorldSource, error) {
	if !clairvoyantAllowed.Load() {
		return nil, ErrClairvoyantRefused
	}
	if e == nil || obs == nil {
		return nil, errors.New("azmcts: the clairvoyant source needs the root engine and observer")
	}
	return &clairvoyant{e: e, obs: obs}, nil
}

func (c *clairvoyant) World(int) (World, error) {
	return World{Engine: c.e.Clone(), Observer: c.obs.Clone()}, nil
}
