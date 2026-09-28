package builtins

// SplitMix64 is a line-for-line port of SpellBench's SplitMix64
// (python/spellbench/builtins/uniform.py, itself ported from mtg-kernel
// python/mtg_kernel_rl/determinism.py). Go's uint64 arithmetic wraps, which
// is exactly the Python original's & MASK64.
type SplitMix64 struct{ state uint64 }

// NewSplitMix64 returns a stream seeded with seed.
func NewSplitMix64(seed uint64) SplitMix64 { return SplitMix64{state: seed} }

// Next returns the stream's next 64-bit value.
func (s *SplitMix64) Next() uint64 {
	s.state += 0x9E3779B97F4A7C15
	z := s.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// Index draws a candidate index in [0, n) with the builtin's modulo
// reduction (stream.next() % len(candidates)). n must be positive.
func (s *SplitMix64) Index(n int) int { return int(s.Next() % uint64(n)) }
