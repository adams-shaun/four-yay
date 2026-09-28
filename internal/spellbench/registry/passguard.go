package registry

// passguard is the worked-example decorator: it delegates every decision to
// the seat it wraps, except one documented case. When the wrapped seat's
// answer at a priority decision is a pass pick, and the decision offers
// exactly one non-pass candidate and that candidate is a land play, the
// pass is replaced with that land play. Everything else -- every other
// decision kind, every non-pass answer, a priority decision with no land
// play, more than one non-pass candidate, or a lone non-pass candidate that
// is not a land play -- goes through untouched.
//
// The replacement never needs payment (a land play is free), so the intent
// keeps the wrapped seat's payment witness (nil at a pass) and only its
// choices change. The wrapped seat is still consulted exactly once, so a
// seed-streamed policy (sb-uniform) draws the same number it would have
// drawn without the decorator.

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

func init() {
	RegisterDecorator("passguard", newPassguard)
}

// newPassguard wraps inner. The wrapper keeps inner's BoardSeat-ness: an
// inner that answers from a botpolicy.Board (the production bot, az) is
// wrapped in a BoardSeat so the engine still hands it the board instead of
// a projected View -- the wrapper never changes which surface inner sees.
func newPassguard(inner seat.Seat, _ uint64) seat.Seat {
	base := passguardSeat{inner: inner}
	if _, ok := inner.(seat.BoardSeat); ok {
		return passguardBoard{base}
	}
	return base
}

// passguardSeat is the plain wrapper over a non-BoardSeat inner.
type passguardSeat struct {
	inner seat.Seat
}

// UnwrapSeat exposes the wrapped seat (the registry's Unwrapper contract):
// a runner that inspects the seat underneath -- the refused-answer fallback
// and stats collection in cmd/botbench -- sees through the decoration.
func (s passguardSeat) UnwrapSeat() seat.Seat { return s.inner }

func (s passguardSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return in, err
	}
	return passguardGuard(d, in), nil
}

// WantsPaymentActions delegates the payment-plan opt-in: the wrapper offers
// exactly what inner offers, so a PaymentPlanConsumer inner still gets its
// plans and a plain inner still gets none.
func (s passguardSeat) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

// passguardBoard is the wrapper over a BoardSeat inner.
type passguardBoard struct {
	passguardSeat
}

func (s passguardBoard) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.(seat.BoardSeat).DecideBoard(ctx, b, d)
	if err != nil {
		return in, err
	}
	return passguardGuard(d, in), nil
}

// passguardGuard applies the one documented replacement. Everything else is
// the intent as inner answered it.
func passguardGuard(d decision.Decision, in decision.Intent) decision.Intent {
	if d.Kind != decision.KPriority || len(in.Choices) != 1 {
		return in
	}
	pass, ok := optionAt(d, in.Choices[0])
	if !ok || pass.Kind != "pass" {
		return in
	}
	lone, count := -1, 0
	for _, o := range d.Options {
		if o.Kind == "pass" {
			continue
		}
		lone, count = o.Index, count+1
	}
	if count != 1 {
		return in
	}
	play, ok := optionAt(d, lone)
	if !ok || play.Kind != "play_land" {
		return in
	}
	in.Choices = []int{lone}
	return in
}

// optionAt finds the decision's option whose Index is idx.
func optionAt(d decision.Decision, idx int) (decision.Option, bool) {
	for _, o := range d.Options {
		if o.Index == idx {
			return o, true
		}
	}
	return decision.Option{}, false
}
