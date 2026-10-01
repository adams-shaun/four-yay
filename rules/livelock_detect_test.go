package rules

import (
	"math/rand/v2"
	"testing"
)

// TestLivelockDetectMatchesBruteForce holds detect's candidate scan (the
// inline second-signature test and the ring walk) to the definition: the
// shortest period p <= n/2 whose trailing 2p signatures are two identical
// halves, over random rings, full (wrapped at every head) and filling.
func TestLivelockDetectMatchesBruteForce(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 20000; trial++ {
		n := 2 + rng.IntN(40)
		sigs := make([]uint64, n)
		alpha := uint64(1 + rng.IntN(3))
		for i := range sigs {
			sigs[i] = rng.Uint64N(alpha)
		}
		head := 0
		if rng.IntN(2) == 0 {
			head = rng.IntN(n)
		}
		// MaxPeriod past the filter's counter range turns the candidate
		// filter off, so the scan itself is what is under test.
		w := livelockWatcher{guard: LoopGuard{MaxPeriod: 1 << 20, CycleEvents: 1 << 30}, sigs: sigs, sigHead: head}
		logical := func(i int) uint64 { return sigs[(head+i)%n] }
		want := 0
		for p := 1; p <= n/2 && want == 0; p++ {
			ok := true
			for j := n - 1; j >= n-p; j-- {
				if logical(j) != logical(j-p) {
					ok = false
					break
				}
			}
			if ok {
				want = p
			}
		}
		w.detect()
		if w.runPeriod != want {
			t.Fatalf("trial %d: ring %v head %d: detect found period %d, want %d", trial, sigs, head, w.runPeriod, want)
		}
	}
}
