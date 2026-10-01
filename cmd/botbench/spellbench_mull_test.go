package main

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestSpellbenchMulligansKnob exercises -spellbench-mulligans: at 1 a game
// must actually reach the London keep/mulligan ask on both seats (the flag is
// the knob that turns Config.Mulligans on in the bench; without the wiring no
// ask is ever posed), and at 0 — the benchmark default — no ask is seen. Both
// shapes must stay seed-deterministic. The seats are the hosted production bot
// (the 1/3 coin) and bot+mull (the hand-quality rule) so the ask is answered
// by the two arms this knob exists to compare.
func TestSpellbenchMulligansKnob(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	deck, err := spellbench.Deck(reg, spellbench.PauperKernel, "Burn")
	if err != nil {
		t.Fatal(err)
	}
	g := sbGame{id: "t", seed: sbGameSeed(20260926, 0, 0), deck: "Burn", seats: [2]string{"bot", "bot+mull"}}
	for _, tc := range []struct {
		mulligans int
		wantAsks  bool
	}{
		{0, false},
		{1, true},
	} {
		t.Run(strconv.Itoa(tc.mulligans), func(t *testing.T) {
			old := sbFlags.mulligans
			sbFlags.mulligans = tc.mulligans
			t.Cleanup(func() { sbFlags.mulligans = old })

			a := sbPlay(g, deck, reg, 200, 20000)
			b := sbPlay(g, deck, reg, 200, 20000)
			if a.err != nil {
				t.Fatalf("mulligans=%d: %v", tc.mulligans, a.err)
			}
			if a.outcome != b.outcome || a.mullAsks != b.mullAsks || a.mullTaken != b.mullTaken {
				t.Fatalf("mulligans=%d: same seed, different games: %+v asks %v taken %v vs %+v asks %v taken %v",
					tc.mulligans, a.outcome, a.mullAsks, a.mullTaken, b.outcome, b.mullAsks, b.mullTaken)
			}
			if a.outcome.IsStalled() {
				t.Fatalf("mulligans=%d: stalled %+v", tc.mulligans, a.outcome)
			}
			asked := a.mullAsks[0] + a.mullAsks[1]
			if (asked > 0) != tc.wantAsks {
				t.Fatalf("mulligans=%d: mulligan asks = %d (per seat %v), want asks>0: %v",
					tc.mulligans, asked, a.mullAsks, tc.wantAsks)
			}
			if tc.wantAsks {
				// The precondition the knob exists to create: each seat was
				// offered the London ask at least once (a seven-card hand).
				for s := 0; s < 2; s++ {
					if a.mullAsks[s] == 0 {
						t.Fatalf("mulligans=1: seat p%d was never asked a keep/mulligan (asks %v)", s, a.mullAsks)
					}
				}
				t.Logf("mulligans=1 asks=%v taken=%v outcome=%s turns=%d intents=%d fallbacks=%v",
					a.mullAsks, a.mullTaken, sbResultLabel(a), a.outcome.Turns, a.outcome.Intents, a.fallbacks)
			} else {
				t.Logf("mulligans=0 asks=%v taken=%v outcome=%s turns=%d", a.mullAsks, a.mullTaken, sbResultLabel(a), a.outcome.Turns)
			}
		})
	}
}
