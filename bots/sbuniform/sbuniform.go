// Package sbuniform hosts the SpellBench uniform builtin.
package sbuniform

import (
	"context"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

const Policy = "sb-uniform"

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name: Policy, Label: "sb-uniform (SpellBench)",
		Description: "The SpellBench uniform policy, answering from the seat's view and using the honest root as its payment planner at priority.",
		Tier:        bots.Experimental,
		Strength:    []bots.Measurement{{Claim: "Anchor 1000 vs bot 1238 on the Pauper kernel", Versus: "bot", Setting: "SpellBench §3.1 W1 table", Source: "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §3.1"}},
		Cost:        bots.Cost{Scope: "per decision", Note: "negligible", Source: "docs/superpowers/specs/2026-09-28-hosted-bot-packages.md §3.2"},
		Env:         true, Formats: []string{"constructed"}, MaxSeats: 2, Caretaker: "bot",
	}, New: New})
}

// NewWithMode constructs the underlying SpellBench seat, preserving the
// benchmark's UniformSeed stream in both the hosted and bench registries.
func NewWithMode(seed uint64, m builtins.ManaMode) *builtins.Seat {
	return builtins.New(builtins.Uniform, m, seed^builtins.UniformSeed)
}

func New(o bots.Options) (seat.Seat, error) {
	mode := builtins.Manual
	if o.AutoPayMana {
		mode = builtins.AutoPay
	}
	return &hostedSeat{bot: NewWithMode(o.Seed, mode)}, nil
}

type hostedSeat struct{ bot *builtins.Seat }

var _ bots.EnvSeat = (*hostedSeat)(nil)
var _ bots.RefusalAnswerer = (*hostedSeat)(nil)
var _ seat.Seat = (*hostedSeat)(nil)

func (s *hostedSeat) WantsEnv(d *decision.Decision) bool { return d.Kind == decision.KPriority }
func (s *hostedSeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	if env.Search.Engine != nil {
		s.bot.SetPlanner(env.Search.Engine)
	} else {
		s.bot.SetPlanner(nil)
	}
	return s.bot.Decide(ctx, env.View, d)
}
func (s *hostedSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.bot.Decide(ctx, v, d)
}
func (s *hostedSeat) WantsPaymentActions() bool { return s.bot.WantsPaymentActions() }

// Seat exposes the wrapped builtin for registry adapters and routing tests.
func (s *hostedSeat) Seat() *builtins.Seat { return s.bot }
func (s *hostedSeat) AnswerRefused(v view.View, d decision.Decision, in decision.Intent) decision.Intent {
	return s.bot.Refused(v, d, in)
}
