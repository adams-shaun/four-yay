// Package azredeal hosts the az-redeal policy (BP-12, spec
// 2026-09-28-hosted-bot-packages §11): the adapter over internal/azmcts.Seat
// in its honest redeal world (generation 0: uniform prior, heuristic leaf,
// 100 simulations, a fresh deal per simulation) that answers the searched
// kinds from the host's honest root and the seat's own observation feed, and
// plays the wrapped default bot everywhere else.
//
// The routing is exactly spec §5.1's adapter rule for `az-redeal`: a decision
// of a searched kind is answered from env.Search when the honest root built
// AND the feed is still live, and from env.Board — the wrapped bot, the
// bench's own fallback — otherwise. The wrapped bot (DecideBoard) is also
// what a non-searched decision costs, so an az-redeal table plays no
// differently from a bot table except at priority, attackers, blockers and
// target decisions.
//
// The adapter never sets SeatConfig.Source: the clairvoyant world source is
// a bench-only injection (internal/azmcts/clairvoyant), and a hosted entry
// with it would clone the real engine.
package azredeal

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/azmcts"
	"github.com/adams-shaun/gorge/internal/policynet"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// Policy is the registry key.
const Policy = "az-redeal"

func init() {
	bots.Register(bots.Entry{Info: bots.Info{
		Name:        Policy,
		Label:       "az-redeal (AlphaZero search)",
		Description: "The az search seat in its honest world: at every searched decision it runs 100 MCTS simulations over freshly redealt worlds rebuilt from its own observations, and plays the production bot everywhere else.",
		Tier:        bots.Experimental,
		Strength: []bots.Measurement{
			{
				Claim:   "70.5% [68.5, 72.5] vs bot (+20.5pp over the 50.0% control)",
				Versus:  "bot",
				Setting: "gorge botbench, az100 redeal (gen 0), 2,000 games, uw-tempo vs the 5 mono decks, true lists visible",
				Source:  "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.5",
			},
			{
				Claim:   "az-redeal-sims25 rated 1382 [1320, 1455] vs bot 1264 on the Pauper kernel",
				Versus:  "bot",
				Setting: "gorge botbench -spellbench, SB's eight-deck seat-swapped mirror pool, Bradley-Terry rating",
				Source:  "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.5",
			},
		},
		Cost: bots.Cost{
			MeanMS: 247, P95MS: 465, Scope: "per searched decision",
			Note:   "216-278 ms mean uncontended at 100 sims (p95 380-550); contention raises it, the host never cuts the search",
			Source: "docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.5",
		},
		Env: true, Search: true,
		Formats:   []string{"constructed"},
		MaxSeats:  2,
		Caretaker: "bot",
	}, New: newAZRedeal})
}

// Hosted is the hosted config (spec §4.3, §11): exactly azmcts's eval seat —
// DefaultSeatConfig(), the 100-sim / Limit 6 / MaxSteps 1000 knobs the +20.5pp
// measurement was taken with — with the honest redeal world and zero K
// (a fresh deal per simulation), the generation-0 shape (no network, no
// exploration noise). World is set explicitly (never the clairvoyant
// default), and Source is left nil: NewSeat refuses a clairvoyant world with
// a nil source, so a hosted seat can never clone the real engine.
func Hosted() azmcts.SeatConfig {
	cfg := azmcts.DefaultSeatConfig()
	cfg.World = azmcts.WorldRedeal
	cfg.Worlds = 0
	return cfg
}

// newAZRedeal is the registry factory: Hosted() with the per-seat seed from
// Options and a nil net (generation 0).
func newAZRedeal(o bots.Options) (seat.Seat, error) {
	return New(o, Hosted())
}

// New builds the adapter over an explicit config overlay. It is botbench's
// entry point (spec §4.3): botbench overlays its -az-* flags on Hosted() and
// calls this; the host goes through the registry factory above. net is
// always nil here — the hosted entry is generation 0 (heuristic leaf,
// uniform prior); a checkpoint would be a different bot.
func New(o bots.Options, cfg azmcts.SeatConfig) (seat.Seat, error) {
	s, err := azmcts.NewSeat(o.Seed, nil, cfg)
	if err != nil {
		return nil, err
	}
	return &hostedSeat{bot: s, cfg: cfg, budgetMS: o.DecisionDeadlineMS}, nil
}

// DecisionBudgetMS is the factory's per-decision wall-clock budget
// (bots.Options.DecisionDeadlineMS), for the host to arm as the DecideEnv
// context's deadline. Only DecideEnv reaches the search here -- Decide and
// DecideBoard forward to the wrapped bot's plain halves, which never build a
// tree -- so the budget is declared on this adapter and armed by the host
// (this package may not import time, internal/archtest).
func (s *hostedSeat) DecisionBudgetMS() int { return s.budgetMS }

// hostedSeat is the adapter. bot is held behind the searchseat.SearchSeat
// interface (which *azmcts.Seat satisfies) so the routing tests can observe
// the forwarding with a recording delegate instead of running real searches.
// budgetMS is the factory's per-decision wall-clock budget
// (bots.Options.DecisionDeadlineMS): the host arms it as its DecideEnv
// context's deadline, and a search that outlives it is aborted by azmcts
// Search's ctx bail-out, which plays the wrapped bot's answer -- this
// policy's non-searched fallback. Zero -- bench and training -- is
// unbounded.
type hostedSeat struct {
	bot      searchseat.SearchSeat
	cfg      azmcts.SeatConfig
	budgetMS int
}

var (
	_ bots.EnvSeat          = (*hostedSeat)(nil)
	_ bots.BudgetedSeat     = (*hostedSeat)(nil)
	_ seat.BoardSeat        = (*hostedSeat)(nil)
	_ searchseat.SearchSeat = (*hostedSeat)(nil)
)

// Decide is the plain Seat half: the wrapped bot. Decisions where WantsEnv is
// false never reach it from the host (the BoardSeat branch serves them), but
// a caller holding the seat only as a Seat goes here.
func (s *hostedSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	return s.bot.Decide(ctx, v, d)
}

// UnwrapSeat lets botbench attach its visit recorder to the underlying az seat.
func (s *hostedSeat) UnwrapSeat() seat.Seat { return s.bot }

// SetRecorder preserves botbench's per-seat az corpus hook through the hosted adapter.
func (s *hostedSeat) SetRecorder(fn func(policynet.VisitRecord)) {
	if recorder, ok := s.bot.(interface {
		SetRecorder(func(policynet.VisitRecord))
	}); ok {
		recorder.SetRecorder(fn)
	}
}

// DecideSearch keeps this adapter usable by botbench's engine-owning driver.
func (s *hostedSeat) DecideSearch(ctx context.Context, env searchseat.Env, d decision.Decision) (decision.Intent, error) {
	return s.bot.DecideSearch(ctx, env, d)
}

// DecideBoard is the game-shaped half: the wrapped bot. It is the fallback
// the Env branch plays (root refused, feed stopped) and the whole answer for
// every non-searched decision.
func (s *hostedSeat) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	return s.bot.DecideBoard(ctx, b, d)
}

// WantsEnv reports whether this decision is one of the az seat's searched
// kinds (priority, attackers, blockers, target, spec §5.1) under the seat's
// own kind set: the host gates its redeal on it, so a config that narrows
// Kinds must narrow the host's redeal cost with it. It is a pure function of
// d and the seat's own config.
func (s *hostedSeat) WantsEnv(d *decision.Decision) bool {
	k := s.cfg.Search.Kinds
	switch d.Kind {
	case decision.KPriority:
		return k.Priority
	case decision.KAttackers:
		return k.Attackers
	case decision.KBlockers:
		return k.Blockers
	case decision.KTarget:
		return k.Target
	default:
		return false
	}
}

// DecideEnv answers one of the seat's own decisions. Spec §5.1: forward
// env.Search to the wrapped DecideSearch when the honest root built AND the
// actor's feed is live; otherwise play the wrapped bot on env.Board — exactly
// the bench's fallback. The gate is not optional decoration: the az seat's
// redeal source is built from the feed's history, so a nil or stopped feed
// reaches it as an error and the az seat fails loudly instead of playing its
// fallback. A nil feed is the host's signal for a stopped feed or a table
// that built none (host/botenv.go envData), the same state Feed.Live() ==
// false reports.
func (s *hostedSeat) DecideEnv(ctx context.Context, env bots.Env, d decision.Decision) (decision.Intent, error) {
	if env.Search.Engine != nil && env.Search.Feed != nil && env.Search.Feed.Live() {
		return s.bot.DecideSearch(ctx, env.Search, d)
	}
	return s.bot.DecideBoard(ctx, env.Board, d)
}
