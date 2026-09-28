package kshadow

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
)

// BenchmarkRollout times one world of sb-tactical rollouts for a pass at
// the recorded priority decision.
func BenchmarkRollout(b *testing.B) {
	s, d := fixture(b, "priority")
	sh := s.Build(&d.Kernel.Obs, Options{Seed: 1, Priority: true})
	look := builtins.NewRegistryLookup(s.Reg)
	r := DefaultRoll()
	r.Worlds, r.Rollout = 1, "tactical"
	acts := []*action{{kind: "pass"}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.evaluate(sh, acts, uint64(i), look)
	}
}
