package main

// The -spellbench registry entries cmd/botbench owns. "bot" needs the
// hosted-policy vocabulary (host.NewBotPolicySeat) and "az" needs this
// package's azmcts wiring (azNet, azCfg, validated by azFrontDoor before any
// game); neither belongs to internal/spellbench/registry, which stays free
// of the command's state. The sb-* builtins are registered by the registry
// package itself (sb.go); see that package's doc for the spec grammar.

import (
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
		s, err := azmcts.NewSeat(seed, azNet, azCfg)
		if err != nil {
			panic("botbench: " + err.Error()) // validated by azFrontDoor before any game
		}
		return s
	})
}
