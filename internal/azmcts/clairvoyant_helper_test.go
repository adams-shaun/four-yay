package azmcts

import (
	"errors"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/rules"
)

// The test-only clairvoyant source. internal/azmcts/clairvoyant imports
// azmcts, so this package's tests cannot import it; they inject this copy
// -- the same clones of the real engine and observer, without the
// AllowClairvoyant gate the real package guards -- through SeatConfig.Source
// and Search's WorldSource. Tests do not reach binaries, and archtest scans
// non-test imports only.
type testClairvoyant struct {
	e   *rules.Engine
	obs *searchprobe.Collector
	// prev is the last world handed out and spare its recycled storage (the
	// real source's same reuse): simulations run one at a time, so the
	// previous world is spent when the next is asked for.
	prev  *rules.Engine
	spare rules.Spare
}

// newTestClairvoyant builds the test source for one Search.
func newTestClairvoyant(e *rules.Engine, obs *searchprobe.Collector) (WorldSource, error) {
	if e == nil || obs == nil {
		return nil, errors.New("azmcts: the test clairvoyant source needs the root engine and observer")
	}
	return &testClairvoyant{e: e, obs: obs}, nil
}

func (c *testClairvoyant) World(int) (World, error) {
	if c.prev != nil {
		c.spare = c.prev.Release()
	}
	c.prev = c.e.CloneInto(&c.spare)
	return World{Engine: c.prev, Observer: c.obs.Clone()}, nil
}

// FixedWorld is the real source's: one engine, its generator carried.
func (c *testClairvoyant) FixedWorld() bool { return true }

// testSeatSource is the SeatConfig.Source the azmcts seat tests inject.
func testSeatSource(env searchseat.Env, obs *searchprobe.Collector) (WorldSource, error) {
	return newTestClairvoyant(env.Engine, obs)
}
