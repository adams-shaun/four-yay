package rules

import (
	"unsafe"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// The decision arena: bump-allocated storage for the decisions an engine
// poses.
//
// Every priority ask allocates its Decision and an exactly-sized Options
// array -- measured on the play rows of enginebench's 2026-10-06 profile at
// ~34% of alloc_space (arenaOptions 27.5%, arenaDecision 7.2%) and the single
// largest GC lever after the event log. An engine switched on with the arena
// carves those decisions out of reusable chunks instead, each Options slice
// capped at its length (an append by any reader reallocates, never touching a
// neighbour), and recycles the chunks rather than letting the GC see them.
//
// The arena is ON by default (rules.New). Its storage is valid under the
// CONTRACT in decision_arena_live.go: a posed *decision.Decision and its
// Options are valid only until the next Submit (or Advance) on the engine
// that posed them; a caller that needs one past that point copies it with
// Decision.Clone(). Two callers' lifetimes decide how the chunks are reset:
//
//   - a SEARCH SIMULATION's engine and every decision it posed die together,
//     so its arena (SetDecisionArena) is Release-scoped: Release clears the
//     chunks and hands them, through the Spare, to the next simulation's
//     clone. It never flips generations.
//   - a LIVE engine never Releases mid-game, so its arena keeps two decision
//     generations and retires one at every Submit boundary
//     (decision_arena_live.go). Its liveness is bounded by construction.
//
// The arena is never copied by Clone: a clone adopts a spent engine's cleared
// chunks through the Spare, switched off, and its owner turns the arena back
// on if its own decisions die with it. A by-value Engine copy (entryPreview's
// preview) never uses the owner's arena (decisionArena.owner).

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
	if decisionArenaVerify {
		// A retired generation may have been poisoned (slab.retire); a slot
		// is only ever handed out zeroed.
		clear(out)
	}
	s.off += n
	return out
}

// tail returns the current chunk's unhanded tail as an empty slice whose
// capacity is the rest of the chunk (moving to a fresh chunk first when
// fewer than min slots remain), for a caller that appends into it in place
// and then hands out a prefix by advancing off. Nothing is handed out until
// then; a caller that wrote into the tail without handing it out must clear
// what it wrote, so every unhanded slot stays zero.
func (s *slab[T]) tail(min, chunk int) []T {
	if s.at < len(s.chunks) && s.off+min > chunk {
		s.at, s.off = s.at+1, 0
	}
	if s.at == len(s.chunks) {
		s.chunks = append(s.chunks, make([]T, chunk))
		s.off = 0
	}
	c := s.chunks[s.at]
	return c[s.off:s.off:len(c)]
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

// arenaGen is one decision generation's storage: the priority/other
// decisions and their Options. A live arena keeps two and alternates them at
// Submit boundaries (decision_arena_live.go).
type arenaGen struct {
	opts slab[decision.Option]
	decs slab[decision.Decision]
}

// decisionArena is one engine's arena (see the file comment). Its decision
// storage is double-buffered for a live engine and Release-scoped for a
// search simulation. Besides the decisions it can back the other
// engine-scoped objects a simulation builds per event or resolution: the
// resolution contexts (effects.Ctx, ~3.5 KB, resolveTop /
// resolveAbilitySacrificing) and the look-back LKI objects (emit's pre-event
// copy, checkTriggers' snapshot copy). Each of those is referenced by the
// resolution, pending triggers, resume frames and parked state of the engine
// that built it, which outlive a Submit, so the res slabs are ALWAYS
// Release-scoped and are only switched on for a search simulation.
type decisionArena struct {
	owner *Engine
	on    bool
	// live selects the live-play contract: the decision slabs rotate through
	// two generations, one retired per Submit. false is a search
	// simulation's arena, reset only at Release.
	live bool
	// res switches the resolution slabs on. They are Release-scoped by
	// nature (see above), so a live engine leaves them off and heap-allocates
	// its Ctx/LKI exactly as an arena-off engine did.
	res  bool
	cur  int
	gens [2]arenaGen
	ctxs slab[effects.Ctx]
	objs slab[state.Object]
}

// SetDecisionArena switches e into SEARCH-SIMULATION arena mode: the priority
// decisions e poses (their Decision and Options), its resolution contexts and
// its LKI copies live in storage that e.Release recycles into the next engine
// CloneInto builds from that Spare, so a caller that turns it on promises
// that nothing read from e -- no decision, no Options slice -- is used after
// e.Release. The search's per-simulation worlds are that shape
// (internal/azmcts's engine env). SetDecisionArena(false) turns the arena off
// entirely. A fresh engine (rules.New) is a LIVE arena instead: decisions are
// bounded per Submit rather than per Release (decision_arena_live.go).
func (e *Engine) SetDecisionArena(on bool) {
	a := e.ownArena()
	a.on, a.live, a.res = on, false, on
}

// ownArena is e's arena, created on first use and replaced if e carries a
// by-value copy's pointer. A fresh engine gets a LIVE arena (on, generation-
// bounded); SetDecisionArena or adoptArena changes its mode.
func (e *Engine) ownArena() *decisionArena {
	if e.decArena == nil || e.decArena.owner != e {
		e.decArena = &decisionArena{owner: e, on: true, live: true}
	}
	return e.decArena
}

// activeArena is e's arena when it is switched on and owned by e, else nil.
// It creates the (live, on) arena on first use so rules.New's default needs
// no explicit switch.
func (e *Engine) activeArena() *decisionArena {
	a := e.decArena
	if a == nil || a.owner != e {
		a = e.ownArena()
	}
	if a.on {
		return a
	}
	return nil
}

// arenaOptions returns a zeroed Options slice of length and capacity n.
func (e *Engine) arenaOptions(n int) []decision.Option {
	if a := e.activeArena(); a != nil && n <= arenaOptChunk {
		return a.gens[a.cur].opts.take(n, arenaOptChunk)
	}
	return make([]decision.Option, n)
}

// arenaOptTailMin is the least room a priority walk builds its options in
// place for (legalActionsWalkWithWindow); a larger walk outgrows the tail
// and falls back to the copy.
const arenaOptTailMin = 96

// optTail is one in-place option build in an arena's option tail.
type optTail struct {
	a       *decisionArena
	buf     []decision.Option
	at, off int
}

// arenaOptionsTail returns the arena's option tail for an in-place build
// (buf nil when the arena is off).
func (e *Engine) arenaOptionsTail() optTail {
	a := e.activeArena()
	if a == nil {
		return optTail{}
	}
	s := &a.gens[a.cur].opts
	buf := s.tail(arenaOptTailMin, arenaOptChunk)
	return optTail{a: a, buf: buf, at: s.at, off: s.off}
}

// commit hands out out, built in place in t.buf, with hw the most slots the
// build ever held: it clears the slots past len(out) the build wrote. When
// out no longer lives in buf (the build outgrew it) it hands out nothing,
// clears everything the build wrote into buf, and reports false; out's own
// array is then untouched.
func (t optTail) commit(out []decision.Option, hw int) bool {
	s := &t.a.gens[t.a.cur].opts
	if s.at != t.at || s.off != t.off {
		// Nothing takes options while a walk builds (only the posed walk
		// itself does), so a moved slab would mean overlapping slots.
		panic("rules: decision arena options taken during an in-place priority walk")
	}
	if len(out) > 0 && unsafe.SliceData(out) == unsafe.SliceData(t.buf[:1]) {
		s.off += len(out)
		if hw > len(out) {
			clear(t.buf[len(out):hw])
		}
		return true
	}
	clear(t.buf[:cap(t.buf)])
	return false
}

// arenaDecision returns a zeroed Decision.
func (e *Engine) arenaDecision() *decision.Decision {
	if a := e.activeArena(); a != nil {
		return a.gens[a.cur].decs.one(arenaDecChunk)
	}
	return &decision.Decision{}
}

// arenaCtx returns a zeroed resolution Ctx.
func (e *Engine) arenaCtx() *effects.Ctx {
	if a := e.activeArena(); a != nil && a.res {
		return a.ctxs.one(arenaCtxChunk)
	}
	return new(effects.Ctx)
}

// arenaObject returns a pointer to a copy of *o (an LKI snapshot). o is
// only read, so a caller's local stays on its stack; only the off-arena
// copy is heap-allocated.
func (e *Engine) arenaObject(o *state.Object) *state.Object {
	if a := e.activeArena(); a != nil && a.res {
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
	for i := range a.gens {
		a.gens[i].opts.reset()
		a.gens[i].decs.reset()
	}
	a.ctxs.reset()
	a.objs.reset()
	a.owner, a.on, a.live, a.res = nil, false, false, false
	a.cur = 0
	return a
}

// adoptArena makes a Spare's cleared arena e's own, switched off.
func (e *Engine) adoptArena(a *decisionArena) {
	if a == nil {
		return
	}
	a.owner, a.on, a.live, a.res = e, false, false, false
	a.cur = 0
	e.decArena = a
}
