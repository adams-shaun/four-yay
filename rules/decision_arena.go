package rules

import "github.com/adams-shaun/gorge/decision"

// The decision arena: bump-allocated storage for the priority decisions a
// SEARCH SIMULATION's engine poses.
//
// Every priority ask allocates its Decision and an exactly-sized Options
// array, and a simulation poses hundreds of them (the largest allocation
// class of the search loop after the look-back snapshots). A simulation's
// engine and every decision it posed die together: the search reads a
// posed decision only until it answers it, and the world is Released when
// the next simulation starts. An engine switched on with SetDecisionArena
// therefore carves its priority decisions out of reusable chunks instead,
// each Options slice capped at its length (an append by any reader
// reallocates, never touching a neighbour), and Release clears the chunks
// and hands them, through the Spare, to the next simulation's clone.
//
// The arena is OFF by default and never copied by Clone: an engine whose
// decisions may be retained past its Release (a hosted table, a bench whose
// traces keep decisions, the real game a search reads) must not switch it
// on. A by-value Engine copy (entryPreview's preview) never uses the owner's
// arena (decisionArena.owner).

// decisionArenaChunk is the Option slots per chunk (~200 KB); a larger
// request, never seen in practice, is allocated on its own.
const decisionArenaChunk = 512

// decisionArenaDecChunk is the Decision slots per chunk.
const decisionArenaDecChunk = 64

type decisionArena struct {
	owner *Engine
	on    bool
	// opts / decs are the chunks, each full length; optAt / decAt index the
	// chunk being carved and optOff / decOff the next free slot in it.
	opts          [][]decision.Option
	optAt, optOff int
	decs          [][]decision.Decision
	decAt, decOff int
}

// SetDecisionArena switches e's decision arena on or off. On, the priority
// decisions e poses (their Decision and Options) live in storage that
// e.Release recycles into the next engine CloneInto builds from that Spare,
// so a caller that turns it on promises that nothing -- no decision, no
// Options slice -- read from e is used after e.Release. The search's
// per-simulation worlds are that shape (internal/azmcts's engine env).
// Posed decisions are otherwise identical to the arena-off ones.
func (e *Engine) SetDecisionArena(on bool) {
	a := e.ownArena()
	a.on = on
}

// ownArena is e's arena, created on first use and replaced if e carries a
// by-value copy's pointer.
func (e *Engine) ownArena() *decisionArena {
	if e.decArena == nil || e.decArena.owner != e {
		e.decArena = &decisionArena{owner: e}
	}
	return e.decArena
}

// activeArena is e's arena when it is switched on and owned by e, else nil.
func (e *Engine) activeArena() *decisionArena {
	if a := e.decArena; a != nil && a.on && a.owner == e {
		return a
	}
	return nil
}

// arenaOptions returns a zeroed Options slice of length and capacity n.
func (e *Engine) arenaOptions(n int) []decision.Option {
	a := e.activeArena()
	if a == nil || n > decisionArenaChunk {
		return make([]decision.Option, n)
	}
	if a.optAt < len(a.opts) && a.optOff+n > decisionArenaChunk {
		a.optAt, a.optOff = a.optAt+1, 0
	}
	if a.optAt == len(a.opts) {
		a.opts = append(a.opts, make([]decision.Option, decisionArenaChunk))
		a.optOff = 0
	}
	c := a.opts[a.optAt]
	s := c[a.optOff : a.optOff+n : a.optOff+n]
	a.optOff += n
	return s
}

// arenaDecision returns a zeroed Decision.
func (e *Engine) arenaDecision() *decision.Decision {
	a := e.activeArena()
	if a == nil {
		return &decision.Decision{}
	}
	if a.decAt < len(a.decs) && a.decOff == decisionArenaDecChunk {
		a.decAt, a.decOff = a.decAt+1, 0
	}
	if a.decAt == len(a.decs) {
		a.decs = append(a.decs, make([]decision.Decision, decisionArenaDecChunk))
		a.decOff = 0
	}
	d := &a.decs[a.decAt][a.decOff]
	a.decOff++
	return d
}

// releaseArena clears the used part of e's arena (dropping every string,
// Grant and slice the dead decisions referenced) and returns its chunks for
// a Spare. Every slot handed out again is therefore zero, as a fresh
// allocation is.
func (e *Engine) releaseArena() (opts [][]decision.Option, decs [][]decision.Decision) {
	a := e.decArena
	if a == nil || a.owner != e {
		return nil, nil
	}
	for i := 0; i < len(a.opts) && i <= a.optAt; i++ {
		clear(a.opts[i])
	}
	for i := 0; i < len(a.decs) && i <= a.decAt; i++ {
		clear(a.decs[i])
	}
	opts, decs = a.opts, a.decs
	e.decArena = nil
	return opts, decs
}

// adoptArena seeds a new engine's (off) arena with a Spare's cleared chunks.
func (e *Engine) adoptArena(opts [][]decision.Option, decs [][]decision.Decision) {
	if len(opts) == 0 && len(decs) == 0 {
		return
	}
	e.decArena = &decisionArena{owner: e, opts: opts, decs: decs}
}
