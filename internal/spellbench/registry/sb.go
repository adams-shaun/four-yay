package registry

// The SpellBench builtin bots, registered under the exact names the
// -spellbench round robin spells (the three plain names at the AutoPay
// surface, plus the -manual and -planned arms; internal/spellbench/builtins'
// package doc records what each surface exposes). The plain factories take
// the caller's mana surface; the -manual/-planned names force theirs, so a
// name always means the same seat wherever it is resolved.
//
// sb-uniform XORs in the benchmark's uniform seed (builtins.UniformSeed),
// as the benchmark configures seed 11 for the uniform bot.

import (
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
)

func init() {
	Register("sb-uniform", func(seed uint64, mana builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Uniform, mana, seed^builtins.UniformSeed)
	})
	Register("sb-heuristic", func(seed uint64, mana builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Heuristic, mana, seed)
	})
	Register("sb-first", func(seed uint64, mana builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.First, mana, seed)
	})
	Register("sb-uniform-manual", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Uniform, builtins.Manual, seed^builtins.UniformSeed)
	})
	Register("sb-heuristic-manual", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Heuristic, builtins.Manual, seed)
	})
	Register("sb-uniform-planned", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Uniform, builtins.Planned, seed^builtins.UniformSeed)
	})
	Register("sb-heuristic-planned", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.New(builtins.Heuristic, builtins.Planned, seed)
	})
}
