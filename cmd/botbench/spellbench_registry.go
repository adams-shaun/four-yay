package main

// The -spellbench registry entries cmd/botbench owns. "bot" needs the
// hosted-policy vocabulary (host.NewBotPolicySeat) and "az" needs this
// package's azmcts wiring (azNet, azCfg, validated by azFrontDoor before any
// game); neither belongs to internal/spellbench/registry, which stays free
// of the command's state. The sb-* builtins are registered by the registry
// package itself (sb.go); see that package's doc for the spec grammar.

import (
	"fmt"

	"github.com/adams-shaun/gorge/host"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/registry"
	"github.com/adams-shaun/gorge/seat"
)

func init() {
	registry.Register("bot", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return hostedPolicy(host.BotPolicy)(seed)
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
		s, err := azmcts.NewSeat(seed, azNet, azSeatConfig("az-redeal"))
		if err != nil {
			panic("botbench: " + err.Error()) // validated by azFrontDoor before any game
		}
		return s
	})
	// The sb-tactical arms (spellbench-prep) are the scored seat-visible
	// heuristic; they read this package's tactical weights and card lookup,
	// so like az they are registered here. The -manual/-planned arms force
	// their mana surface; the -no<group> arms switch one idea group off and
	// the -alt arms play -spellbench-tactical-alt-weights (A/B tuning).
	registry.Register("sb-tactical", func(seed uint64, _ builtins.ManaMode) seat.Seat {
		return builtins.NewTactical(builtins.AutoPay, seed, tacticalLookup, tacticalWeights)
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
