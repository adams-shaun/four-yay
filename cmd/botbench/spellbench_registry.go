package main

// The -spellbench registry entries cmd/botbench owns. "bot" needs the
// hosted-policy vocabulary (host.NewBotPolicySeat) and "az" needs this
// package's azmcts wiring (azNet, azCfg, validated by azFrontDoor before any
// game); neither belongs to internal/spellbench/registry, which stays free
// of the command's state. The sb-* builtins are registered by the registry
// package itself (sb.go); see that package's doc for the spec grammar.

import (
	"fmt"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/bots/azredeal"
	hostedsbsearch "github.com/adams-shaun/gorge/bots/sbsearch"
	"github.com/adams-shaun/gorge/bots/sbtactical"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/seat"
)

func init() {
	registry.Register("bot", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return hostedPolicy(bots.Default)(seed)
	})
	registry.Register("az", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		s, err := azmcts.NewSeat(seed, azNet, azSeatConfig("az"))
		if err != nil {
			panic("botbench: " + err.Error()) // validated by azFrontDoor before any game
		}
		return s
	})
	// az-redeal is az on the honest world source (azmcts.RedealSource): every
	// simulation walks a world that keeps what the seat sees and re-deals the
	// hidden cards it cannot. It takes every -az-* knob but -az-world, so one
	// run can seat it beside a clairvoyant az.
	registry.Register("az-redeal", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		s, err := azredeal.New(benchBotOptions(seed), azRedealOverlay())
		if err != nil {
			panic("botbench: " + err.Error())
		}
		return s
	})
	// The sb-tactical arms (spellbench-prep) are the scored seat-visible
	// heuristic; they read this package's tactical weights and card lookup,
	// so like az they are registered here. The -manual/-planned arms force
	// their mana surface; the -no<group> arms switch one idea group off and
	// the -alt arms play -spellbench-tactical-alt-weights (A/B tuning).
	registry.Register("sb-tactical", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		s, err := sbtactical.New(benchBotOptions(seed), tacticalOverlay(sbtactical.Hosted()))
		if err != nil {
			panic("botbench: " + err.Error())
		}
		return s
	})
	registry.Register("sb-tactical-planned", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.NewTactical(builtins.Planned, seed, tacticalLookup, tacticalWeights)
	})
	registry.Register("sb-tactical-noearly", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		w := tacticalWeights
		w.EarlyGame = false
		return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, w)
	})
	registry.Register("sb-tactical-notiming", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		w := tacticalWeights
		w.Timing = false
		return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, w)
	})
	registry.Register("sb-tactical-norace", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		w := tacticalWeights
		w.Race = false
		return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, w)
	})
	registry.Register("sb-tactical-arch", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, tacticalArchW)
	})
	// sb-search (internal/spellbench/sbsearch): determinized search over
	// honest redealt worlds, sb-tactical as the candidate prior and as both
	// sides' rollout policy. The variants differ in budget only.
	for _, v := range sbSearchVariants {
		cfg := v.cfg
		cfg.Name = v.name
		registry.Register(v.name, func(seed uint64, _ builtins.ManaMode) seat.Seat {
			if v.name == hostedsbsearch.Policy {
				var err error
				seat, err := hostedsbsearch.New(benchBotOptions(seed), sbSearchOverlay(hostedsbsearch.LiteAtk()))
				if err != nil {
					panic("botbench: " + err.Error())
				}
				return seat
			}
			return sbsearch.New(builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, tacticalWeights), seed, cfg)
		})
	}
	for i := 0; i < len(tacticalAltWeights); i++ {
		i := i
		name := "sb-tactical-alt"
		if i > 0 {
			name = fmt.Sprintf("sb-tactical-alt%d", i+1)
		}
		registry.Register(name, func(seed uint64, _ builtins.ManaMode) seat.Seat {
			return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, tacticalAltWeights[i])
		})
	}
}

// sbSearchVariants are the registered sb-search budgets.
var sbSearchVariants = func() []struct {
	name string
	cfg  sbsearch.Config
} {
	d := sbsearch.DefaultConfig()
	with := func(f func(*sbsearch.Config)) sbsearch.Config { c := d; f(&c); return c }
	return []struct {
		name string
		cfg  sbsearch.Config
	}{
		{"sb-search", d},
		{"sb-search-w0", with(func(c *sbsearch.Config) { c.Worlds = 0 })},
		{"sb-search-w8", with(func(c *sbsearch.Config) { c.Worlds = 8 })},
		{"sb-search-w32", with(func(c *sbsearch.Config) { c.Worlds = 32 })},
		{"sb-search-h4", with(func(c *sbsearch.Config) { c.Horizon = 4 })},
		{"sb-search-fast", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon = 8, 3 })},
		{"sb-search-fast-atk", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack = 8, 3, true })},
		{"sb-search-lite", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon = 4, 2 })},
		{"sb-search-lite-atk", hostedsbsearch.LiteAtk()},
		{"sb-search-atk", with(func(c *sbsearch.Config) { c.Attack = true })},
		// sb-search2: more decision kinds and adaptive budgets on lite-atk.
		{"sb-search-lite-atk-blk", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack, c.Block = 4, 2, true, true })},
		{"sb-search-lite-atk-tgt", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack, c.Target = 4, 2, true, true })},
		{"sb-search-lite-atk-bt", with(func(c *sbsearch.Config) {
			c.Worlds, c.Horizon, c.Attack, c.Block, c.Target = 4, 2, true, true, true
		})},
		// -rec8 is lite-atk at W=8 (its first 4 worlds are lite-atk's): the
		// per-world values it records drive the offline budget study.
		{"sb-search-lite-atk-fl", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack, c.Leaf = 4, 2, true, sbsearch.LeafFitted })},
		{"sb-search-lite-atk-aw", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack, c.AttackWide = 4, 2, true, 4 })},
		{"sb-search-lite-atk-rec8", with(func(c *sbsearch.Config) { c.Worlds, c.Horizon, c.Attack = 8, 2, true })},
	}
}()
