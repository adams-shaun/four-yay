package bench

import (
	"sync"

	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/seat"
)

// SparePool recycles finished games' log and object arrays (rules.Spare)
// between the games a batch runner plays back to back. It is the ONE place
// the pool and the release-after-last-use rule live: a runner draws a Spare,
// passes it as Config.Spare, and returns the finished engine exactly once,
// after the last reader of that engine is done. Which spare a game draws is
// scheduling-dependent but invisible (rules.Spare's contract), so a run's
// output is byte-identical with or without recycling
// (TestSpareReuseIsInvisible).
//
// A SparePool is safe for concurrent use: sync.Pool hands each worker its own
// Spare, so two live games never share one. A single Spare shared across
// workers IS a data race -- the 2026-10-05 sampler bug fixed in 75c053d09 --
// so a caller that needs one spare per worker must give each worker its own
// pool, never share a *rules.Spare value.
type SparePool struct {
	pool sync.Pool
}

// Get returns a Spare ready to pass as Config.Spare. The zero Spare is
// "none", so a first call (an empty pool) is as correct as a recycled one.
func (p *SparePool) Get() *rules.Spare {
	if s, _ := p.pool.Get().(*rules.Spare); s != nil {
		return s
	}
	return new(rules.Spare)
}

// Put retires a finished engine and returns its storage to the pool. It MUST
// be the engine's last use: no reader of e.L, e.G or a posed decision may
// outlive this call, and nothing may hold a *decision.Decision across it
// (rules.Spare's contract). sp is the Spare the engine was built with (the
// value Get returned, passed as Config.Spare); Release fills it and it goes
// back for the next game.
func (p *SparePool) Put(sp *rules.Spare, e *rules.Engine) {
	*sp = e.Release()
	p.pool.Put(sp)
}

// PlayGameRecycled plays one game on a pooled Spare and returns the engine's
// storage to the pool. lastUse runs while the engine is still valid and MUST
// be its last read (nil is fine when nothing reads it after the game). The
// engine is not returned: a caller whose last read is more than a plain
// callback should draw a Spare with Get and Put it itself after that read.
//
// A game that ERRORED is not recycled: the engine is dropped to the GC
// rather than returning arrays it still points at. That matches the contract
// the hand-rolled pools already followed (a game with no trustworthy log
// shape is skipped), and it is the safe direction -- the cost is one
// forgone reuse, never a shared array.
func (p *SparePool) PlayGameRecycled(cfg rules.Config, seats []seat.Seat, maxTurns, maxIntents int, hooks Hooks, lastUse func(*rules.Engine)) (Outcome, error) {
	sp := p.Get()
	cfg.Spare = sp
	o, e, err := PlayGame(cfg, seats, maxTurns, maxIntents, hooks)
	if err != nil || e == nil {
		return o, err
	}
	if lastUse != nil {
		lastUse(e)
	}
	p.Put(sp, e)
	return o, nil
}
