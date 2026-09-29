package lethalpressure

import (
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/seat"
)

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name: "lethal-pressure", Label: "Lethal Pressure", Description: "The manual hosted bot policy; on auto-pay tables it uses its own decision rules rather than the production bot's combat-simulation arm.", Tier: bots.Experimental,
		Strength: []bots.Measurement{{Claim: "51.65% [50.10, 53.20] vs the pre-AR7 bot", Versus: "pre-AR7 bot", Setting: "gorge botbench, mono suite, held-out, manual mana", Source: "docs/superpowers/reports/2026-09-19-bot-policy-decision-trace-and-ar7.md — Held-out gate"}},
		Cost:     bots.Cost{Scope: "per decision", Note: "as bot", Source: "docs/superpowers/specs/2026-09-28-hosted-bot-packages.md §3.1"},
		Formats:  []string{"constructed", "commander"}, Caretaker: "bot",
	}, New: newLethalPressure})
}

func newLethalPressure(o bots.Options) (seat.Seat, error) {
	b := seat.NewLethalPressureBot(o.Seed)
	if o.AutoPayMana {
		b.EnableAutoPayMana()
	}
	return b, nil
}
