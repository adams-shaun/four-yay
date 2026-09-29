// Package sbtactical hosts the sb-tactical policy (BP-13, spec
// 2026-09-28-hosted-bot-packages §11): the adapter over
// internal/spellbench/builtins.NewTactical (the scored seat-visible
// heuristic behind SpellBench's sb-tactical arm) that rides the host's Env
// path for its planner and plays the plain View path everywhere else.
//
// The routing is exactly spec §5.1's adapter rule for `sb-tactical`: a
// decision of kind priority is answered from env.View with the honest root
// installed as the seat's planner (SetPlanner) — or with the planner
// explicitly nil when the root was refused — and every other decision is
// answered through the plain Seat path, where the host projects exactly the
// View a non-Env seat would get. The planner is INFERRED (builtins package
// doc, 1) to be read at priority only; this package's test measures the
// claim (BP-13), because a read at any other kind would consult a root
// built for an earlier decision.
//
// The adapter never sees the live engine: the host's Env carries only the
// honest root (host/botenv.go envData), and the planner derives the actor's
// own pool and sources from it. Priority is the only kind the host builds an
// Env for here (WantsEnv), so an sb-tactical table costs one honest redeal
// per priority decision and nothing else beyond a plain bot.
package sbtactical

import (
	"context"
	"fmt"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Policy is the registry key.
const Policy = "sb-tactical"

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name:        Policy,
		Label:       "sb-tactical (SpellBench tactical heuristic)",
		Description: "The SpellBench scored heuristic in its hosted shape: at priority it prices its plays through the honest root as its payment planner, and answers every other decision from the seat's own view — creature values, timing, race and removal scoring over what the seat can legally see, card facts resolved from the served card registry.",
		Tier:        bots.Experimental,
		Strength: []bots.Measurement{
			{
				Claim:   "110-18 on the Pauper kernel (benchmark seed); 106-22 at seed 99991; 87-41 on FDN mirrors",
				Versus:  "bot",
				Setting: "SpellBench §3.4 'Measured' table: Pauper-kernel and FDN-limited mirrors vs the sb bot",
				Source:  "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §3.4",
			},
			{
				Claim:   "Elo 1507 vs bot 1242",
				Versus:  "bot",
				Setting: "SpellBench §3.4 'Measured' table, Pauper kernel",
				Source:  "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §3.4",
			},
		},
		Cost: bots.Cost{
			MeanMS: 1, Scope: "per decision (INFERRED)",
			Note:   "~80 ms per GAME vs bot on 8 workers (SB §3.4); per-decision think time never measured, ~1 ms INFERRED",
			Source: "docs/superpowers/specs/2026-09-28-hosted-bot-packages.md §3.2",
		},
		Env: true, Search: false,
		Formats:   []string{"constructed"},
		MaxSeats:  2,
		Caretaker: "bot",
	}, New: newSBTactical})
}

// Hosted is the hosted configuration the registry entry serves: the tactical
// weights exactly as SpellBench measured them (SB §3.4). botbench's
// tactical-weights flags overlay this (BP-16), so the hosted default and the
// bench's default arm stay one function.
func Hosted() builtins.TacticalWeights { return builtins.DefaultTacticalWeights() }

// New builds the hosted seat from bots.Options and an overlay of Hosted().
// The mana mode is always AutoPay (spec §3.2's table row — the shape the SB
// §3.4 measurements were taken with) and the seed is bots.Options.Seed, so
// the same seat answers a hosted table and a bench game identically.
func New(o bots.Options, w builtins.TacticalWeights) (seat.Seat, error) {
	if o.Deps.Cards == nil {
		return nil, fmt.Errorf("%s: Deps.Cards is nil: the sb-tactical heuristic reads printed card facts from the served card registry", Policy)
	}
	lookup := builtins.NewRegistryLookup(o.Deps.Cards)
	return &hostedSeat{bot: builtins.NewTactical(builtins.AutoPay, o.Seed, lookup, w)}, nil
}

// newSBTactical is the registry factory: Hosted() with no overlay.
func newSBTactical(o bots.Options) (seat.Seat, error) {
	return New(o, Hosted())
}

// hostedSeat is the adapter. bot is the wrapped builtins seat.
type hostedSeat struct {
	bot *builtins.Seat
}

var (
	_ bots.EnvSeat         = (*hostedSeat)(nil)
	_ bots.RefusalAnswerer = (*hostedSeat)(nil)
	_ seat.Seat            = (*hostedSeat)(nil)
)

// wantsEnvFor is WantsEnv's one home, kept off the seat so the registry's
// prototype (a nil receiver) can answer it too.
func wantsEnvFor(d *decision.Decision) bool {
	return d.Kind == decision.KPriority
}

// WantsEnv reports whether this decision should ride the host's Env path.
// The sb-tactical seat reads its planner at priority only (INFERRED from the
// builtins package doc and measured by this package's test), so priority is
// the only kind where the honest root buys the seat anything — every other
// kind is answered from the plain View path the host projects anyway.
// It is a pure function of d.
func (s *hostedSeat) WantsEnv(d *decision.Decision) bool {
	return wantsEnvFor(d)
}

// DecideEnv answers one priority decision. Spec §5.1: install the honest
// root as the seat's planner when it built, and the explicit nil when it was
// refused — explicit, because a nil *rules.Engine inside the Planner
// interface is not a nil planner and the seat's read is an interface
// comparison. The seat then answers from env.View, exactly the projection a
// plain Seat gets.
//
// The root stays installed until the next priority decision's DecideEnv
// replaces it: the sb-tactical lowering re-plans the SAME play from "a fresh
// plan from the same decision" (builtins package doc, 1) when a lowering
// aborts mid-payment, and the planner that lowers a play is the planner the
// play was chosen with. The root is honest — an EnvSeat never sees the live
// engine — so holding it costs the seat-privacy boundary nothing; the
// no-retain rule guards env.Search itself, whose Feed and Engine this
// adapter keeps no other reference to.
func (s *hostedSeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	if env.Search.Engine != nil {
		s.bot.SetPlanner(env.Search.Engine)
	} else {
		s.bot.SetPlanner(nil)
	}
	return s.bot.Decide(ctx, env.View, d)
}

// Decide is the plain Seat half: the wrapped heuristic on the View the host
// projected. Every non-priority decision comes here, plus any caller holding
// the seat only as a Seat.
func (s *hostedSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.bot.Decide(ctx, v, d)
}

// WantsPaymentActions is the wrapped seat's (AutoPay: yes).
func (s *hostedSeat) WantsPaymentActions() bool { return s.bot.WantsPaymentActions() }

// AnswerRefused is the refusal ladder's rung 1: the wrapped seat's own
// refusal answer, unchanged. builtins.Seat.Refused already has exactly the
// RefusalAnswerer shape, so the delegation is verbatim.
func (s *hostedSeat) AnswerRefused(v view.View, d decision.Decision, refused decision.Intent) decision.Intent {
	return s.bot.Refused(v, d, refused)
}

// Seat exposes the wrapped builtins seat: the spellbench registry's
// Unwrapper contract shape, and the test's handle for the planner routing
// assertions. Production callers have no use for it — every public entry
// point above routes through the adapter.
func (s *hostedSeat) Seat() *builtins.Seat { return s.bot }
