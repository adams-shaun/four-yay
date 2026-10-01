package rules

import (
	"math/rand/v2"
	"testing"
)

// TestLivelockDetectMatchesBruteForce holds detect's candidate index (the
// same-slot chain, the inline second-signature test) to the definition: the
// shortest period p <= min(MaxPeriod, n/2) whose trailing 2p signatures are
// two identical halves. Random streams over tiny alphabets (so periods and
// slot collisions are common) are pushed past the ring's capacity, and every
// prefix is checked, filling and wrapped.
func TestLivelockDetectMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 400; trial++ {
		maxPeriod := 1 + rng.IntN(12)
		w := livelockWatcher{guard: LoopGuard{MaxPeriod: maxPeriod, CycleEvents: 1 << 30}}
		alpha := uint64(1 + rng.IntN(3))
		var hist []uint64
		for step := 0; step < 6*maxPeriod+10; step++ {
			// Multiples of livelockCandSlots collide in the index on purpose.
			sig := rng.Uint64N(alpha) * livelockCandSlots
			if rng.IntN(4) == 0 {
				sig++
			}
			w.pushSig(sig)
			hist = append(hist, sig)
			win := hist
			if c := 2 * maxPeriod; len(win) > c {
				win = win[len(win)-c:]
			}
			n := len(win)
			want := 0
			for p := 1; p <= min(maxPeriod, n/2) && want == 0; p++ {
				ok := true
				for j := n - 1; j >= n-p; j-- {
					if win[j] != win[j-p] {
						ok = false
						break
					}
				}
				if ok {
					want = p
				}
			}
			if got := w.detectIndexed(); got != want {
				t.Fatalf("trial %d step %d: window %v: indexed detect %d, want %d", trial, step, win, got, want)
			}
			if got := w.detectScan(); got != want {
				t.Fatalf("trial %d step %d: window %v: scan %d, want %d", trial, step, win, got, want)
			}
		}
	}
}
