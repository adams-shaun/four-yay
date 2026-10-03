// Package clairvoyant is azmcts's stage-1 world source: every simulation
// walks a Clone of the REAL engine, hidden zones and future chance
// included. It is bench and training only -- botbench injects it as
// azmcts.SeatConfig.Source -- and nothing that seats a non-bench opponent
// (host, host/httpapi, cmd/gorged, bots/...) may link it
// (internal/archtest). A clairvoyant arm therefore needs both a link-time
// import and a runtime opt-in: NewClairvoyant refuses until the driving
// command called AllowClairvoyant.
package clairvoyant

import (
	"errors"
	"sync/atomic"

	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
)

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
	// prev is the last world handed out and spare its recycled storage:
	// simulations run one at a time, so the previous world is spent when the
	// next is asked for, and its log, arena and memo arrays build this one
	// (rules.CloneInto; reuse is invisible to the game).
	prev  *rules.Engine
	spare rules.Spare
}

// NewClairvoyant is the stage-1 source: every simulation walks a Clone of
// the real engine e, with a Clone of obs. obs must be the collector Search
// is given as Root.Observer; World clones it lazily, so every clone is taken
// after Search has observed the root decision.
func NewClairvoyant(e *rules.Engine, obs *searchprobe.Collector) (azmcts.WorldSource, error) {
	if !clairvoyantAllowed.Load() {
		return nil, ErrClairvoyantRefused
	}
	if e == nil || obs == nil {
		return nil, errors.New("azmcts: the clairvoyant source needs the root engine and observer")
	}
	return &clairvoyant{e: e, obs: obs}, nil
}

func (c *clairvoyant) World(int) (azmcts.World, error) {
	if c.prev != nil {
		c.spare = c.prev.Release()
	}
	c.prev = c.e.CloneInto(&c.spare)
	return azmcts.World{Engine: c.prev, Observer: c.obs.Clone()}, nil
}

// FixedWorld is true: every world is a clone of the one real engine, its
// generator position included, so the search's node cache applies
// (azmcts.FixedWorldSource).
func (c *clairvoyant) FixedWorld() bool { return true }

// RealWorld is true: every world is a clone of the real engine, generator
// included, and gets its own observer clone (tree reuse,
// azmcts.RealWorldSource).
func (c *clairvoyant) RealWorld() bool { return true }

// Source is the SeatConfig.Source a bench or training command injects for
// the clairvoyant world: NewClairvoyant on the decision's root engine and
// observer. azmcts itself never links this package, so a seat configured
// for the clairvoyant world without an injected source is refused at
// NewSeat ("the clairvoyant world is not linked").
func Source(env searchseat.Env, obs *searchprobe.Collector) (azmcts.WorldSource, error) {
	return NewClairvoyant(env.Engine, obs)
}
