// Package seat is who answers the engine's decisions. It sits above rules in
// the dependency order (cards -> state -> decision -> events -> effects ->
// rules -> view -> seat -> replay -> cmd/*): a Seat is handed a view.View,
// never a *state.Game or a *rules.Engine, so it only ever sees what a real
// client would.
package seat

import (
	"context"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"
)

// Seat is anything that answers decisions: a bot, a scripted test seat, or
// (later) a WebSocket-backed human. ctx lets a human seat time out; a bot
// ignores it and never returns an error (Ruling P8).
type Seat interface {
	Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error)
}

// PaymentPlanConsumer opts into the lazy payment-action extension. Hosts and
// tools ensure offers only for seats that explicitly request them.
type PaymentPlanConsumer interface {
	WantsPaymentActions() bool
}

// BoardSeat is a seat that can answer from a botpolicy.Board and needs no
// projected View. host builds the Board under the engine lock and skips
// view.Project entirely for such a seat. Implementing it is an opt-in: a
// seat that does not is still handed a full View through Decide.
// decision.Decision keeps the options copy host already makes.
type BoardSeat interface {
	Seat
	DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error)
}
