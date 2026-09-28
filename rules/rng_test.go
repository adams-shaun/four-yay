package rules

import "testing"

// TestRngCloneContinuesFromTheSamePosition pins rng.clone at the unit level,
// independent of any Engine plumbing: a clone taken after some draws must
// produce the exact same sequence of further draws as the original would
// have, and must start its own Draws counter from the position it was
// cloned at, not from zero.
func TestRngCloneContinuesFromTheSamePosition(t *testing.T) {
	t.Parallel()
	r := newRNG(5)
	for i := 0; i < 17; i++ {
		r.IntN(1000)
	}
	c := r.clone()
	if c.Draws != r.Draws {
		t.Fatalf("clone Draws = %d, want %d (the original's count at clone time)", c.Draws, r.Draws)
	}

	for i := 0; i < 10; i++ {
		got, want := c.IntN(1000), r.IntN(1000)
		if got != want {
			t.Fatalf("draw %d: clone drew %d, original drew %d -- clone did not continue from the same PCG position", i, got, want)
		}
	}
}
