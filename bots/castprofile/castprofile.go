package castprofile

import (
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/seat"
)

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name: "cast-profile", Label: "Cast Profile", Description: "Plays the production bot with a cast profile; the embedded default profile is intent-identical to the manual bot.", Tier: bots.Experimental,
		Strength: []bots.Measurement{{Claim: "Intent-identical to the manual bot with the embedded default profile; fitted L4 profile failed its gate at 50.92% [49.38, 52.47]", Versus: "manual bot", Setting: "embedded default profile; fitted L4 evaluated in the TS mono suite", Source: "cmd/botbench/main.go:188-192; docs/superpowers/reports/2026-09-19-bot-policy-decision-trace-and-ar7.md §2"}},
		Cost:     bots.Cost{Scope: "per decision", Note: "as bot; fitted profile is not embedded", Source: "docs/superpowers/specs/2026-09-28-hosted-bot-packages.md §3.1"},
		Formats:  []string{"constructed", "commander"}, Caretaker: "bot",
	}, New: newCastProfile})
}

func newCastProfile(o bots.Options) (seat.Seat, error) {
	b, err := seat.NewCastProfileBot(o.Seed)
	if err != nil {
		return nil, err
	}
	if o.AutoPayMana {
		b.EnableAutoPayMana()
	}
	return b, nil
}
