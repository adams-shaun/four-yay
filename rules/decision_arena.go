package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

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

// Chunk sizes (slots): Options ~200 KB, Decisions ~47 KB, resolution Ctx
// ~110 KB, look-back LKI objects ~64 KB. A request larger than a chunk,
// never seen in practice, is allocated on its own.
const (
	arenaOptChunk = 512
	arenaDecChunk = 64
	arenaCtxChunk = 32
	arenaObjChunk = 64
)

// slab is one bump-allocated, chunked arena of T. Slots are handed out
// zeroed (fresh chunks are zero, and reset clears every used slot), chunks
// are never moved, so a pointer or slice handed out stays valid until reset.
type slab[T any] struct {
	chunks  [][]T
	at, off int
}

// take returns n zeroed slots, length and capacity n.
func (s *slab[T]) take(n, chunk int) []T {
	if s.at < len(s.chunks) && s.off+n > chunk {
		s.at, s.off = s.at+1, 0
	}
	if s.at == len(s.chunks) {
		s.chunks = append(s.chunks, make([]T, chunk))
		s.off = 0
	}
	c := s.chunks[s.at]
	out := c[s.off : s.off+n : s.off+n]
	s.off += n
	return out
}

// one returns a pointer to one zeroed slot.
func (s *slab[T]) one(chunk int) *T { return &s.take(1, chunk)[0] }

// reset clears every slot handed out and rewinds, keeping the chunks. The
// current chunk was handed out only below off (its tail is still zero), so
// only that prefix is cleared; an earlier chunk is cleared whole.
func (s *slab[T]) reset() {
	for i := 0; i < len(s.chunks) && i <= s.at; i++ {
		if i == s.at {
			clear(s.chunks[i][:s.off])
			break
		}
		clear(s.chunks[i])
	}
	s.at, s.off = 0, 0
}

// decisionArena is one engine's simulation arena (see the file comment).
// Besides the priority decisions it backs the other engine-scoped objects
// a simulation builds per event or resolution: the resolution contexts
// (effects.Ctx, ~3.5 KB, resolveTop / resolveAbilitySacrificing) and the
// look-back LKI objects (emit's pre-event copy, checkTriggers' snapshot
// copy). Each is referenced only by the resolution, pending triggers,
// resume frames and parked state of the engine that built it, all of which
// die at its Release.
type decisionArena struct {
	owner *Engine
	on    bool
	opts  slab[decision.Option]
	decs  slab[decision.Decision]
	ctxs  slab[effects.Ctx]
	objs  slab[state.Object]
}

// SetDecisionArena switches e's decision arena on or off. On, the priority
// decisions e poses (their Decision and Options), its resolution contexts
// and its LKI copies live in storage that e.Release recycles into the next
// engine CloneInto builds from that Spare, so a caller that turns it on
// promises that nothing read from e -- no decision, no Options slice -- is
// used after e.Release. The search's per-simulation worlds are that shape
// (internal/azmcts's engine env). Everything else is identical to the
// arena-off engine.
func (e *Engine) SetDecisionArena(on bool) {
	e.ownArena().on = on
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
	if a := e.activeArena(); a != nil && n <= arenaOptChunk {
		return a.opts.take(n, arenaOptChunk)
	}
	return make([]decision.Option, n)
}

// arenaDecision returns a zeroed Decision.
func (e *Engine) arenaDecision() *decision.Decision {
	if a := e.activeArena(); a != nil {
		return a.decs.one(arenaDecChunk)
	}
	return &decision.Decision{}
}

// arenaCtx returns a zeroed resolution Ctx.
func (e *Engine) arenaCtx() *effects.Ctx {
	if a := e.activeArena(); a != nil {
		return a.ctxs.one(arenaCtxChunk)
	}
	return &effects.Ctx{}
}

// arenaObject returns a pointer to a copy of *o (an LKI snapshot). o is
// only read, so a caller's local stays on its stack; only the off-arena
// copy is heap-allocated.
func (e *Engine) arenaObject(o *state.Object) *state.Object {
	if a := e.activeArena(); a != nil {
		p := a.objs.one(arenaObjChunk)
		*p = *o
		return p
	}
	cp := *o
	return &cp
}

// releaseArena clears the used part of e's arena (dropping every string,
// Grant and slice the dead objects referenced) and detaches it for a Spare.
// Every slot handed out again is therefore zero, as a fresh allocation is.
func (e *Engine) releaseArena() *decisionArena {
	a := e.decArena
	e.decArena = nil
	if a == nil || a.owner != e {
		return nil
	}
	a.opts.reset()
	a.decs.reset()
	a.ctxs.reset()
	a.objs.reset()
	a.owner, a.on = nil, false
	return a
}

// adoptArena makes a Spare's cleared arena e's own, switched off.
func (e *Engine) adoptArena(a *decisionArena) {
	if a == nil {
		return
	}
	a.owner, a.on = e, false
	e.decArena = a
}
