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
//
// The per-game rules.New / NewStartingPlayerChoice census (2026-10-06) and
// why each caller does or does not draw from a SparePool here:
//
//   - recycled: cmd/enginebench playRandom/playBot (the -row random and -row
//     bot rows), cmd/botbench playMatchTraced and sbPlay, cmd/policytune's
//     pair player, cmd/mtgsim playOne, cmd/keywordbench play.
//   - NOT recycled, no single last-use point: cmd/enginebench findRoots (its
//     roots are e.Clone()s that share e's log prefix, so e must outlive
//     them), cmd/enginebench searchGame (the engine is handed to the search
//     and to searchseat.Feed, whose results may retain clones), cmd/hindsight
//     captureGame (branches retain e.Clone() for post-game evaluation),
//     cmd/searchteacher playGame (returns the engine to its caller),
//     internal/paymirror PlayConfig (a deferred handler reads e.G at return
//     and CheckLive may retain), internal/spellbench kshadow/v2engine/v2shadow
//     (the engine outlives the "game" as the observer/translator pipeline).
//   - already owns spare recycling: internal/searchprobe (per goroutine since
//     75c053d09; its verifyActual rebuilds a replay that owns its output),
//     internal/azmcts (clairvoyant/nodecache/redeal/seat/world).
//   - out of scope or by construction: host/ engines outlive the game
//     (feedback capture and replay); replay/ builds the engine that produces
//     its own output log; cmd/autopayaudit and cmd/cardfuzz read the engine
//     across several post-game phases (verify replay, failure context).
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
// A game that ERRORED or that ended in an engine-bug ABORT is not recycled:
// the engine is dropped to the GC rather than returning arrays it still
// points at. That matches the contract the hand-rolled pools already
// followed (a game with no trustworthy log shape is skipped -- botbench's
// pool and spellbench both guard with IsAbort), and it is the safe direction
// -- the cost is one forgone reuse, never a shared array.
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
	// An engine-bug abort (panic/livelock) may have left the engine
	// mid-mutation; drop it rather than recycle.
	if IsAbort(o.StallOn) {
		return o, nil
	}
	p.Put(sp, e)
	return o, nil
}
