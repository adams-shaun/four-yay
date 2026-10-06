package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// TestArenaRelocateDecisionHandlesChunkOversize pins that relocating a
// deferred decision never slices past an option chunk. drainDeferredAsks
// (ask.go) relocates a decision that was built while a commander-zone ask was
// pending into the arena's current generation; a target/choose walk's option
// list can outgrow arenaOptChunk, and the in-place walk and arenaOptions both
// fall back to a heap array in that case, so the relocation must do the same
// rather than call opts.take(n, arenaOptChunk) unguarded (which panics on a
// slice past the chunk). The oversize case is the regression: before the
// guard it panicked, so this test cannot pass vacuously.
func TestArenaRelocateDecisionHandlesChunkOversize(t *testing.T) {
	e := arenaGame(t)
	a := e.activeArena()
	if a == nil || !a.on || !a.live {
		t.Fatal("precondition: the engine does not carry a live, on arena")
	}

	for _, n := range []int{1, arenaOptChunk, arenaOptChunk + 1, 2*arenaOptChunk + 3} {
		src := &decision.Decision{
			Kind:    decision.KTarget,
			Player:  0,
			Prompt:  "relocate me",
			Options: make([]decision.Option, n),
		}
		for i := range src.Options {
			src.Options[i] = decision.Option{Index: i, Label: "opt"}
		}
		nd := arenaRelocateDecision(a, src)
		if nd == nil {
			t.Fatalf("n=%d: arenaRelocateDecision returned nil", n)
		}
		if nd == src {
			t.Fatalf("n=%d: the decision was not relocated into the arena", n)
		}
		if nd.Kind != src.Kind || nd.Prompt != src.Prompt {
			t.Fatalf("n=%d: header not copied: got %v/%q want %v/%q", n, nd.Kind, nd.Prompt, src.Kind, src.Prompt)
		}
		if len(nd.Options) != n {
			t.Fatalf("n=%d: relocated option count = %d", n, len(nd.Options))
		}
		for i := range nd.Options {
			if nd.Options[i].Index != i || nd.Options[i].Label != "opt" {
				t.Fatalf("n=%d: option %d not copied: %#v", n, i, nd.Options[i])
			}
		}
		if n <= arenaOptChunk {
			// A chunk-sized request is carved from the slab: the copy must be
			// independent of the source, never an alias.
			if &nd.Options[0] == &src.Options[0] {
				t.Fatalf("n=%d: the relocated options alias the source array", n)
			}
		}
	}
}
