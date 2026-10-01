// Package sbsearch hosts the sb-search-lite-atk policy (BP-14, spec
// 2026-09-28-hosted-bot-packages §11): the adapter over
// internal/spellbench/sbsearch.Seat, the honest determinized search that
// plays sb-tactical's pick unless a candidate it dealt four worlds for beats
// that pick by the margin (lite: W4, a 2-turn horizon with the material leaf,
// attacks searched).
//
// sb-search is a searchseat.SearchSeat built around an inner sb-tactical
// builtins.Seat. The adapter holds that inner seat directly so its fallback
// is exactly spec §5.1's rule for this entry: when the honest root did not
// build or the actor's feed is stopped, it plays the INNER sb-tactical on
// env.View with the planner set to nil -- never sbsearch.Seat.DecideBoard,
// which projects a view from whatever planner engine the seat happens to
// carry (a stale engine, or none) instead of from the honest Env.
//
// The inner tactical's planner is never installed by this adapter. The
// search reads the honest root from env.Search.Engine itself (the redeal
// base), and its rollouts clone the inner seat and hand the clone the world
// it plays in (sbsearch.Seat.rollout), so a hosted seat never needs a
// planner on the live seat at all -- and §5.1's builtins rule (SetPlanner
// the honest root) therefore does not apply here.
//
// The adapter never sees the live engine: the host's Env carries only the
// honest root (host/botenv.go envData), and env.View is the actor's own
// projection. Priority and attackers are the only kinds WantsEnv claims
// (§5.1), so an sb-search-lite-atk table costs one honest redeal per
// priority-or-attack decision and a plain View answer everywhere else.
package sbsearch

import (
	"context"
	"fmt"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/bots/sbtactical"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/internal/spellbench/builtins"
	"github.com/adams-shaun/gorge/internal/spellbench/sbsearch"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Policy is the registry key.
const Policy = "sb-search-lite-atk"

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name:        Policy,
		Label:       "sb-search lite-atk (SpellBench search)",
		Description: "The honest determinized search: at priority it prices sb-tactical's best play plus its next best candidates over four redealt worlds rolled forward two turns with sb-tactical on both sides, searches an attack declaration the same way, and answers every other decision from the seat's own view as plain sb-tactical.",
		Tier:        bots.Experimental,
		Strength: []bots.Measurement{{
			Claim:   "328-184 (64.1%) vs sb-tactical",
			Versus:  "sb-tactical",
			Setting: "gorge botbench -spellbench, SpellBench Pauper kernel, eight-deck seat-swapped mirrors, seed 777, 512 games",
			Source:  "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.6",
		}},
		Cost: bots.Cost{
			MeanMS: 219, Scope: "per searched decision",
			Note:   "p90 448 ms, p99 1,108 ms per searched decision on a box at load 14-20 (about 85 searched decisions per game per seat); 5.2% of decisions overridden",
			Source: "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.6",
		},
		Env: true, Search: true,
		Formats:   []string{"constructed"},
		MaxSeats:  2,
		Caretaker: "bot",
	}, New: newSBSearch})
}

// LiteAtk is the registered sb-search budget (spec §11 BP-14): the
// cmd/botbench spellbench registry's "sb-search-lite-atk" row -- W4, a
// 2-turn horizon, attacks searched -- with this entry's Name so the search's
// Diags label the hosted policy. Hosted() and botbench share this one
// function, so the hosted default and the bench's lite-atk arm cannot drift
// (BP-16 wires botbench to it).
func LiteAtk() sbsearch.Config {
	cfg := sbsearch.DefaultConfig()
	cfg.Name = Policy
	cfg.Worlds, cfg.Horizon, cfg.Attack = 4, 2, true
	return cfg
}

// Hosted is the hosted config: exactly LiteAtk().
func Hosted() sbsearch.Config { return LiteAtk() }

// newSBSearch is the registry factory: Hosted() over the per-seat seed.
func newSBSearch(o bots.Options) (seat.Seat, error) {
	return New(o, Hosted())
}

// New builds the host adapter over an explicit config overlay. The host goes
// through the registry factory above; botbench builds its bench drive through
// NewBench, which wraps this seat with the driver-facing SearchSeat half.
//
// The inner sb-tactical seat is built with the hosted sb-tactical weights
// (bots/sbtactical.Hosted()) and this entry's per-seat seed, exactly as
// sbsearch.New's own callers build it. With no card registry the tactical
// heuristic cannot resolve printed card facts, so the factory refuses.
func New(o bots.Options, cfg sbsearch.Config) (seat.Seat, error) {
	s, err := newHostedSeat(o, cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// NewBench builds the bench-facing adapter: New's hostedSeat wrapped in the
// bench-only searchseat.SearchSeat half (spec §5.1 / BP-16). It is botbench's
// entry point (spec §4.3): botbench overlays its -search-* flags on Hosted()
// and calls this; the driver's feed branch type-asserts the returned seat for
// searchseat.SearchSeat. The host never calls it -- bots.New returns the bare
// hostedSeat from New above, which deliberately does not satisfy either
// searchseat.SearchSeat or seat.BoardSeat (§5.1's plain-View contract).
func NewBench(o bots.Options, cfg sbsearch.Config) (seat.Seat, error) {
	s, err := newHostedSeat(o, cfg)
	if err != nil {
		return nil, err
	}
	return &benchSearchSeat{hostedSeat: s}, nil
}

// newHostedSeat is the shared construction New and NewBench warm their
// per-seat adapter from.
func newHostedSeat(o bots.Options, cfg sbsearch.Config) (*hostedSeat, error) {
	if o.Deps.Cards == nil {
		return nil, fmt.Errorf("%s: Deps.Cards is nil: the sb-search inner sb-tactical reads printed card facts from the served card registry", Policy)
	}
	lookup := builtins.NewRegistryLookup(o.Deps.Cards)
	inner := builtins.NewTactical(builtins.AutoPay, o.Seed, lookup, sbtactical.Hosted())
	return &hostedSeat{bot: sbsearch.New(inner, o.Seed, cfg), inner: inner, budgetMS: o.DecisionDeadlineMS}, nil
}

// DecisionBudgetMS is the factory's per-decision wall-clock budget
// (bots.Options.DecisionDeadlineMS), for the host to arm as the DecideEnv
// context's deadline. Only DecideEnv reaches the search here -- Decide
// answers from the plain View path -- so the budget is declared on this
// adapter and armed by the host (this package may not import time,
// internal/archtest).
func (s *hostedSeat) DecisionBudgetMS() int { return s.budgetMS }

// hostedSeat is the adapter. bot is the wrapped search seat; inner is the
// sb-tactical seat underneath it (bots' UnwrapSeat returns it), held
// separately so the fallback and the nil-planner install reach it without a
// type assertion per decision. budgetMS is the factory's per-decision
// wall-clock budget (bots.Options.DecisionDeadlineMS): the host arms it as
// its DecideEnv context's deadline, and a search that outlives it stops
// between worlds (sbsearch's ctx bail-out) and plays sb-tactical's pick --
// this policy's non-searched fallback. Zero -- bench and training -- is
// unbounded.
type hostedSeat struct {
	bot      *sbsearch.Seat
	inner    *builtins.Seat
	budgetMS int
}

var (
	_ bots.EnvSeat             = (*hostedSeat)(nil)
	_ bots.BudgetedSeat        = (*hostedSeat)(nil)
	_ bots.RefusalAnswerer     = (*hostedSeat)(nil)
	_ seat.PaymentPlanConsumer = (*hostedSeat)(nil)
	_ seat.Seat                = (*hostedSeat)(nil)
)

// wantsEnvFor is WantsEnv's one home, kept off the seat so it is a pure
// function of d (spec §5.1: an EnvSeat's WantsEnv must be pure). sb-search
// searches priority decisions and, with Attack, attackers declarations;
// nothing else is ever searched, so nothing else is worth an honest redeal.
func wantsEnvFor(d *decision.Decision) bool {
	return d.Kind == decision.KPriority || d.Kind == decision.KAttackers
}

// WantsEnv reports whether this decision should ride the host's Env path:
// priority or attackers (spec §5.1). Every other kind is answered from the
// plain View path the host projects anyway.
func (s *hostedSeat) WantsEnv(d *decision.Decision) bool { return wantsEnvFor(d) }

// DecideEnv answers one of the seat's own decisions. Spec §5.1: forward
// env.Search to the wrapped DecideSearch when the honest root built AND the
// actor's feed is live; otherwise play the inner sb-tactical on env.View
// with the planner explicitly cleared -- never DecideBoard, whose own
// fallback would be the default bot from a stale planner projection. The
// gate is the same one sbsearch.Seat.redealer applies internally (a nil or
// stopped feed refuses every world); applying it here keeps the decision's
// answer in this package, where the fallback rule is documented.
func (s *hostedSeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	if env.Search.Engine != nil && env.Search.Feed != nil && env.Search.Feed.Live() {
		return s.bot.DecideSearch(ctx, env.Search, d)
	}
	s.inner.SetPlanner(nil)
	return s.bot.Decide(ctx, env.View, d)
}

// Decide is the plain Seat half: the inner sb-tactical on the View the host
// projected. Every non-Env decision comes here, plus any caller holding the
// seat only as a Seat. The planner is cleared first so a plain-path answer
// can never price a play from an engine this adapter did not build.
func (s *hostedSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	s.inner.SetPlanner(nil)
	return s.bot.Decide(ctx, v, d)
}

// WantsPaymentActions is the wrapped seat's (AutoPay: yes): the inner
// sb-tactical reads the host's payment plans, so the host must offer them.
func (s *hostedSeat) WantsPaymentActions() bool { return s.bot.WantsPaymentActions() }

// AnswerRefused is the refusal ladder's rung 1: the inner sb-tactical's own
// refusal answer, reached through UnwrapSeat (spec §11 BP-14) so the ladder
// asks the seat that actually answered rather than a re-wrapped copy.
// builtins.Seat.Refused has exactly the RefusalAnswerer shape, so the
// delegation is verbatim.
func (s *hostedSeat) AnswerRefused(v view.View, d decision.Decision, refused decision.Intent) decision.Intent {
	inner, ok := s.bot.UnwrapSeat().(*builtins.Seat)
	if !ok {
		// Unreachable: New always wraps a *builtins.Seat. Refusing the
		// original answer keeps the ladder's rung 1 deterministic rather
		// than panicking mid-match.
		return refused
	}
	return inner.Refused(v, d, refused)
}

// benchSearchSeat is the bench-only view of hostedSeat: the driver-facing
// searchseat.SearchSeat half (spec §5.1 / BP-16). It exists so hostedSeat
// itself never satisfies seat.BoardSeat -- DecideBoard's signature is
// byte-identical to seat.BoardSeat's only method, so a hostedSeat carrying it
// would be type-asserted by any host code looking for a BoardSeat and routed
// through DecideBoard instead of the intended Decide/DecideEnv path. The host
// therefore only ever receives the plain hostedSeat from bots.New; the bench
// driver, whose SearchSeat branch REQUIRES DecideBoard for a stopped feed,
// gets this wrapper.
//
// It embeds *hostedSeat, so every host-facing method (WantsEnv, DecideEnv,
// Decide, DecisionBudgetMS, WantsPaymentActions, AnswerRefused) is promoted
// unchanged; only the two bench methods and UnwrapSeat are declared here.
type benchSearchSeat struct {
	*hostedSeat
}

var (
	_ bots.EnvSeat             = (*benchSearchSeat)(nil)
	_ bots.BudgetedSeat        = (*benchSearchSeat)(nil)
	_ bots.RefusalAnswerer     = (*benchSearchSeat)(nil)
	_ seat.PaymentPlanConsumer = (*benchSearchSeat)(nil)
	_ seat.Seat                = (*benchSearchSeat)(nil)
	// The bench driver's SearchSeat contract (BP-16): internal/bench.PlayGame
	// builds an observation feed for a SearchSeat and answers its own
	// decisions from DecideSearch — the live engine with -hosted-root off
	// (the bench keeps the real engine), the honest root with it on. The host
	// never calls it; its Env dispatch routes the same wrapped call through
	// DecideEnv, gated on WantsEnv. Only the bench wrapper satisfies this: the
	// host's hostedSeat deliberately does not, since DecideBoard is also
	// seat.BoardSeat's method.
	_ searchseat.SearchSeat = (*benchSearchSeat)(nil)
)

// DecideSearch is the bench driver's SearchSeat contract (BP-16):
// internal/bench.PlayGame answers the adapter's own decisions from its
// observation feed — the live engine with -hosted-root off (the bench keeps
// the real engine), the honest root with it on. The host never calls it; its
// Env dispatch routes the same wrapped call through DecideEnv, gated on
// WantsEnv.
func (s *benchSearchSeat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	return s.bot.DecideSearch(ctx, env, d)
}

// DecideBoard is the bench driver's other SearchSeat half: the wrapped search
// seat's own board fallback, exactly as the bare *sbsearch.Seat answered a
// non-live feed before BP-16. The host never calls this (its Env dispatch
// plays the inner sb-tactical on env.View with a nil planner through
// DecideEnv); this method exists so botbench's existing SearchSeat branch
// drives the same wrapped seat with the real engine the bench keeps.
// Deliberately declared on benchSearchSeat and NOT on hostedSeat: the latter
// would then structurally satisfy seat.BoardSeat.
func (s *benchSearchSeat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	return s.bot.DecideBoard(ctx, b, d)
}

// UnwrapSeat exposes the wrapped search seat: botbench's existing SpellBench
// hooks (the planner hand-off, the fallback's sb-tactical assertion, the
// stats read) reach the inner seat through registry.UnwrapSeat's chain,
// exactly as they reached the bare *sbsearch.Seat the bench registered
// before BP-16. Without it the bench's SetPlanner(e) silently misses the
// inner seat and its decisions change under it. Like the two bench methods,
// it belongs to the bench wrapper, not to the hostedSeat robots.New returns.
func (s *benchSearchSeat) UnwrapSeat() seat.Seat { return s.bot }
