package effects

import "testing"

// allocsPerRun is testing.AllocsPerRun taken as the minimum of three
// measurements. The compiled-parameter front caches (cloneFront, voteFront,
// ...) are direct-mapped package globals, so any concurrent compile in the
// test binary that lands on the same slot evicts the primed entry and the
// next lookup recompiles: an allocation the code under test does not make
// in steady state. A real allocation shows in every measurement; an
// eviction does not repeat three times running.
func allocsPerRun(runs int, f func()) float64 {
	best := testing.AllocsPerRun(runs, f)
	for i := 0; i < 2 && best != 0; i++ {
		if n := testing.AllocsPerRun(runs, f); n < best {
			best = n
		}
	}
	return best
}
