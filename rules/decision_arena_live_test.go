package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestLiveArenaStaysBounded pins the live arena's central promise
// (decision_arena_live.go): an engine that is never Released mid-game must
// not accumulate decision storage as the game runs. The arena keeps two
// generations and retires one at every Submit, so the chunk count is a
// function of the largest single generation, not of the number of Submits. A
// per-ask arena with no era boundary would need ~ceil(total options in the
// game / arenaOptChunk) chunks; after 600 priority decisions that is dozens,
// while the era-bounded arena stays in single digits.
func TestLiveArenaStaysBounded(t *testing.T) {
	e := arenaGame(t)
	a := e.activeArena()
	if a == nil || !a.live {
		t.Fatal("precondition: a fresh engine is not a live arena")
	}
	if a.gens[0].opts.chunks == nil && a.gens[1].opts.chunks == nil {
		t.Fatal("precondition: the live arena has not carved any chunks")
	}
	bot := newTestBot(3)
	play := func(n int) {
		t.Helper()
		for i := 0; i < n && !e.G.Over && e.Pending() != nil; i++ {
			if err := e.Submit(bot.answer(e, e.Pending())); err != nil {
				t.Fatalf("Submit %d: %v", i, err)
			}
		}
	}
	counts := func() (opts, decs int) {
		a := e.activeArena()
		return len(a.gens[0].opts.chunks) + len(a.gens[1].opts.chunks),
			len(a.gens[0].decs.chunks) + len(a.gens[1].decs.chunks)
	}
	// Two equal windows: a bounded arena reuses its chunks, so the count is
	// stable (the board grows, but only within one generation's chunk need).
	play(300)
	opts1, decs1 := counts()
	play(300)
	opts2, decs2 := counts()
	if opts2 != opts1 || decs2 != decs1 {
		t.Fatalf("the live arena grew across 300 further Submits: opts chunks %d -> %d, decs chunks %d -> %d",
			opts1, opts2, decs1, decs2)
	}
	if opts2 > 8 || decs2 > 8 {
		t.Fatalf("the live arena is not era-bounded: opts chunks %d, decs chunks %d (a per-ask arena over 600 decisions needs dozens)",
			opts2, decs2)
	}
}

// TestLiveArenaPoisonsRetainedDecision proves the verify mode catches a caller
// that kept a decision past the next Submit -- the class the contract
// (decision_arena_live.go) makes illegal. The rules test binary switches the
// poison on in init (decision_arena_verify_test.go); the precondition fails
// loudly if it is off, so the test can never pass vacuously.
func TestLiveArenaPoisonsRetainedDecision(t *testing.T) {
	if !decisionArenaVerify {
		t.Fatal("precondition: the arena poison verify mode is off")
	}
	e := arenaGame(t)
	if a := e.activeArena(); a == nil || !a.live {
		t.Fatal("precondition: a fresh engine is not a live arena")
	}
	d0 := e.Pending()
	if d0 == nil || d0.Kind != decision.KPriority || len(d0.Options) == 0 {
		t.Fatalf("precondition: no priority decision with options (%v)", d0)
	}
	// slabOpts is the header into the arena's option slab, the storage the
	// contract retires. detached is an owned copy for the differed check.
	slabOpts := d0.Options
	detached := append([]decision.Option(nil), d0.Options...)
	// The first Submit answers d0 and retires d0's generation before it
	// returns, so every slot d0 references is poisoned.
	if err := e.Submit(newTestBot(3).answer(e, d0)); err != nil {
		t.Fatal(err)
	}
	if d0.Kind != "arena-poison" {
		t.Fatalf("the answered decision slot was not poisoned: Kind = %q", d0.Kind)
	}
	for i := range detached {
		if slabOpts[i].Index != -1 {
			t.Fatalf("retained option %d was not poisoned: %#v", i, slabOpts[i])
		}
		if slabOpts[i].Label == detached[i].Label {
			t.Fatalf("retained option %d still carries its label %q", i, slabOpts[i].Label)
		}
	}
	if e.Pending() == nil {
		t.Fatal("precondition: the game ended after one Submit")
	}
}
