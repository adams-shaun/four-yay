package host

import (
	"context"
	"time"

	"github.com/adams-shaun/gorge/bots"
	_ "github.com/adams-shaun/gorge/bots/bot"
	"github.com/adams-shaun/gorge/seat"
)

const (
	BotPolicy            = bots.BotPolicy
	LethalPressurePolicy = bots.LethalPressurePolicy
	CastProfilePolicy    = bots.CastProfilePolicy
)

// NormalizeBotPolicy returns a hosted policy's stable name.
func NormalizeBotPolicy(name string) (string, error) { return bots.Normalize(name) }

// NewBotPolicySeat builds a fresh deterministic seat for a supported hosted policy.
func NewBotPolicySeat(name string, seed uint64) (seat.Seat, error) {
	return NewBotPolicySeatWithAutoPayMana(name, seed, false)
}

// newCaretakerSeat builds the timeout caretaker for a human seat. deps is
// the embedder's card dependency (host.Options.BotDeps), threaded so a
// corpus-hungry policy can still build its caretaker on a served table.
func newCaretakerSeat(name string, seed uint64, autoPayMana bool, deps bots.Deps) (seat.Seat, error) {
	s, err := NewBotPolicySeatWithDeps(name, seed, autoPayMana, deps)
	if err != nil {
		return nil, err
	}
	if b, ok := s.(*seat.Bot); ok && autoPayMana {
		b.SkipLifePlans()
	}
	return s, nil
}

// NewBotPolicySeatWithAutoPayMana builds a named hosted bot. Its public
// signature is load-bearing (cmd/cardfuzz and tests) and carries no card
// dependency: policies whose factory needs one refuse here, and the host's
// own paths build through NewBotPolicySeatWithDeps with host.Options.BotDeps.
func NewBotPolicySeatWithAutoPayMana(name string, seed uint64, autoPayMana bool) (seat.Seat, error) {
	return NewBotPolicySeatWithDeps(name, seed, autoPayMana, bots.Deps{})
}

// NewBotPolicySeatWithDeps builds a named hosted bot with the embedder's
// card dependency (BP-13): every bots.New the host makes carries
// host.Options.BotDeps, so a policy whose factory reads card facts — the
// sb-tactical heuristic (bots/sbtactical) — builds on a served table and
// refuses, with its own error, when a caller builds it bare.
func NewBotPolicySeatWithDeps(name string, seed uint64, autoPayMana bool, deps bots.Deps) (seat.Seat, error) {
	return bots.New(name, bots.Options{Seed: seed, AutoPayMana: autoPayMana, Deps: deps, DecisionDeadlineMS: hostedDecisionDeadlineMS})
}

// hostedDecisionDeadlineMS is the per-decision wall-clock budget the host
// arms on every hosted search seat's DecideEnv context (bots.Options.
// DecisionDeadlineMS, turned into the deadline by decisionCtx; bench and
// training code build the same policies with a zero budget and stay
// unbounded). 2000 is chosen from the seats' own measured costs, so it
// almost never fires under normal load and only cuts a contention pile-up --
// the live-demo freeze of several tables' searches landing at once at
// match-end that this bail-out exists for: az-redeal measures 247 ms mean,
// 465 ms p95 uncontended per searched decision at 100 sims (its Info.Cost),
// so 2000 is ~4.3x that p95; sb-search-lite-atk measures 219 ms mean and a
// p99 of 1,108 ms per searched decision ON A BOX AT LOAD 14-20 (its
// Info.Cost), so 2000 is ~1.8x that loaded p99.
const hostedDecisionDeadlineMS = 2000

// decisionCtx arms s's per-decision wall-clock budget (bots.BudgetedSeat,
// Options.DecisionDeadlineMS) on ctx: a context with the deadline when the
// seat declares a positive budget, ctx itself with a no-op cancel otherwise
// (bench-built seats, zero budget: unbounded, byte-identical behaviour). A
// search that outlives the deadline is aborted by the search's own bail-out
// and the policy's non-searched fallback answers; the deadline never fails a
// decision.
func decisionCtx(ctx context.Context, s seat.Seat) (context.Context, context.CancelFunc) {
	if b, ok := s.(bots.BudgetedSeat); ok {
		if ms := b.DecisionBudgetMS(); ms > 0 {
			return context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		}
	}
	return ctx, func() {}
}
