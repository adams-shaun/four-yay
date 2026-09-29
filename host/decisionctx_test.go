package host

import (
	"context"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/seat"
)

// budgetSeat is a stand-in declaring a per-decision wall-clock budget, the
// shape the adapters' hosted seats have (bots.BudgetedSeat).
type budgetSeat struct {
	seat.Seat
	ms int
}

func (b budgetSeat) DecisionBudgetMS() int { return b.ms }

// TestDecisionCtxArmsTheSeatBudget pins the host's half of the per-decision
// wall-clock budget (hostedDecisionDeadlineMS): a seat that declares a
// positive budget (the bots.Register'd search seats built through
// host/bot_policy.go and the table path) gets its DecideEnv context armed
// with that deadline; a seat with a zero budget -- bench and training build
// the same factories unbounded -- gets the caller's context unchanged.
func TestDecisionCtxArmsTheSeatBudget(t *testing.T) {
	start := time.Now()
	cctx, cancel := decisionCtx(context.Background(), budgetSeat{ms: hostedDecisionDeadlineMS})
	defer cancel()
	dl, ok := cctx.Deadline()
	if !ok {
		t.Fatal("decisionCtx armed nothing on a seat that declares a budget")
	}
	if got := dl.Sub(start); got <= 0 || got > time.Duration(hostedDecisionDeadlineMS)*time.Millisecond+50*time.Millisecond {
		t.Fatalf("armed deadline is %v from start, want ~%d ms", got, hostedDecisionDeadlineMS)
	}

	plain, pcancel := decisionCtx(context.Background(), budgetSeat{ms: 0})
	defer pcancel()
	if _, ok := plain.Deadline(); ok {
		t.Fatal("decisionCtx armed a deadline on a zero-budget seat; bench and training must stay unbounded")
	}
	if _, ok := context.Background().Deadline(); ok {
		t.Fatal("the unarmed path returned a different context: it must pass the caller's through unchanged")
	}
}
