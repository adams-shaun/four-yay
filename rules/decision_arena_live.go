package rules

import "github.com/adams-shaun/gorge/decision"

// The live-play half of the decision arena (decision_arena.go): the era
// machine that bounds an arena which is never Released mid-game.
//
// A search simulation switches an arena on with SetDecisionArena(true) and
// Releases the engine every few hundred decisions, so its decision slabs are
// simply cleared at Release. A LIVE engine -- a hosted table, enginebench's
// play rows, any game driven to the end -- never Releases until the game is
// over, so a slab reset only at Release would grow with the whole game. The
// arena's live mode ("a.live", the default a fresh Engine carries) instead
// keeps TWO decision generations and makes the CONTRACT below the reset
// point.
//
// CONTRACT (operator decision, 2026-10-06). A posed *decision.Decision, its
// Options slice and everything they reference are valid only until the next
// Submit (or Advance) on the engine that posed them. A caller that needs a
// decision past that point must copy it, with decision.Decision.Clone() or an
// equivalent deep copy.
//
// Where the reset happens, and why it is safe:
//
//   - Every asked decision is allocated from gens[a.cur]. At the point
//     submitCommit clears e.pending (the decision being answered has been
//     validated: Submit's validation is a pure read that runs first), the
//     arena flips cur to the other generation. The handlers and the tail
//     Advance that follow pose any new decisions there, while d -- the
//     answered decision the handlers still read -- stays in the generation
//     just left behind, untouched.
//   - At the end of submitCommit, once handlers and Advance are done with d,
//     the left-behind generation is retired: cleared (releasing the strings
//     and slices the dead decisions held), or, in the poison verify mode,
//     overwritten with a sentinel. The retire is DEFERRED (arenaFlipEra) so
//     it also runs when a tape resolution unwinds out of submitCommit with a
//     panic; a flip whose retire was skipped would leave the slab growing.
//     So a decision posed in era k is retired at the end of the Submit that
//     follows it: it is valid through the whole next Submit, which is
//     strictly more than the contract promises.
//   - A decision that is DEFERRED behind a commander-zone ask
//     (drainDeferredAsks) is relocated into the current generation when it is
//     finally posed, so a deferred decision is never left in a generation the
//     next Submit will retire.
//
// liveness is bounded: cur alternates every Submit, and each Submit retires
// exactly the generation it flipped away from, so at any instant at most two
// generations' worth of chunks exist, whatever the game length.

// decisionArenaVerify makes the arena's contract self-checking: the retired
// generation's slots are overwritten with a sentinel at every Submit
// boundary, and every slot handed out is cleared first (a retired generation
// may have been poisoned). A reader that retained a decision past the next
// Submit then sees the sentinel instead of a silently reused slot, and a test
// that compares it against the detached copy it took when the decision was
// posed fails loudly.
//
// Set by the rules test binary (decision_arena_verify_test.go) and by the
// host and view test binaries through SetDecisionArenaVerify.
var decisionArenaVerify = false

// SetDecisionArenaVerify turns the arena's poison verification on. It is the
// one exported door to decisionArenaVerify, for the host and view test
// binaries, which cannot set the rules package's unexported var directly.
func SetDecisionArenaVerify(on bool) { decisionArenaVerify = on }

// poisonOptions overwrites retired option slots with a sentinel that no posed
// decision can hold (the valid option Index range is [0, len)).
func poisonOptions(s []decision.Option) {
	for i := range s {
		s[i] = decision.Option{
			Index: -1,
			Label: "rules: decision arena generation retired before the next Submit " +
				"(a decision retained past the contract)",
		}
	}
}

// poisonDecisions overwrites retired decisions with a sentinel Kind and
// Prompt; the Options are dropped as well, since the slot may be re-handed
// out.
func poisonDecisions(s []decision.Decision) {
	for i := range s {
		s[i] = decision.Decision{
			Kind: "arena-poison",
			Prompt: "rules: decision arena generation retired before the next Submit " +
				"(a decision retained past the contract)",
		}
	}
}

// retire clears (or, in verify mode, poisons) every slot the generation
// handed out and rewinds it for reuse. The chunks stay, so no allocation
// happens on the next era.
func (s *slab[T]) retire(poison func([]T)) {
	if decisionArenaVerify {
		for i := 0; i < len(s.chunks) && i <= s.at; i++ {
			if i == s.at {
				poison(s.chunks[i][:s.off])
				break
			}
			poison(s.chunks[i])
		}
		s.at, s.off = 0, 0
		return
	}
	s.reset()
}

// arenaFlipEra is submitCommit's entry into the next decision generation. It
// flips a live arena to the other generation and returns a function that
// retires the generation just left behind (the answered decisions, which no
// live reader may touch any more). Only a live arena (the default) has
// generations; a search simulation's arena is Release-scoped and never flips.
//
// The caller DEFERS the returned retire. A tape resolution can unwind out of
// submitCommit with a panic (the resolve kernel suspends at a stop-ask), and
// an unwind that skipped the retire would leave the flip unmatched: the next
// re-execution flips again, so neither generation is ever cleared and the
// arena grows with the game. Deferring the retire keeps the flip and its
// clear inseparable on every exit path.
func arenaFlipEra(a *decisionArena) func() {
	if a == nil || !a.live {
		return func() {}
	}
	old := a.cur
	a.cur = 1 - a.cur
	return func() {
		a.gens[old].opts.retire(poisonOptions)
		a.gens[old].decs.retire(poisonDecisions)
	}
}

// arenaRelocateDecision copies d into the arena's current generation. A
// decision posed while a commander-zone ask was pending (ask's deferral) may
// sit in a generation the next Submit will retire before it is ever posed;
// drainDeferredAsks relocates it so every pending decision lives in the
// current generation. The caller passes e.activeArena() (nil off the arena),
// so the owner has already been checked.
func arenaRelocateDecision(a *decisionArena, d *decision.Decision) *decision.Decision {
	if a == nil || !a.on || !a.live || d == nil {
		return d
	}
	nd := a.gens[a.cur].decs.one(arenaDecChunk)
	*nd = *d
	// Only a request that fits a chunk may be carved from the option slab,
	// exactly as arenaOptions decides: a larger walk was never arena-backed
	// (arenaOptions and the in-place tail both fall back to a heap array), so
	// d.Options is already heap and nd's copy of the pointer (above) is the
	// safe fallback. Calling opts.take unguarded would slice past the chunk
	// and panic.
	if n := len(d.Options); n > 0 && n <= arenaOptChunk {
		opts := a.gens[a.cur].opts.take(n, arenaOptChunk)
		copy(opts, d.Options)
		nd.Options = opts
	}
	return nd
}
