package azredeal

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
)

// TestHostedAZDeclaresTheHostedDecisionDeadline pins the budget wiring: the
// registry factory stores the host's per-decision wall-clock budget
// (bots.Options.DecisionDeadlineMS) on the adapter for the host to arm as its
// DecideEnv context's deadline (bots.BudgetedSeat), and a bench-style build
// -- the same factory, a zero budget -- declares none and stays unbounded.
// The bail-out itself is pinned at azmcts level (internal/azmcts
// bailout_test.go); this file pins that the hosted path is the one that arms
// it.
func TestHostedAZDeclaresTheHostedDecisionDeadline(t *testing.T) {
	hosted, err := New(bots.Options{Seed: 5, DecisionDeadlineMS: 2000}, Hosted())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := hosted.(bots.BudgetedSeat).DecisionBudgetMS(); got != 2000 {
		t.Fatalf("DecisionBudgetMS = %d, want the factory's 2000", got)
	}
	bench, err := New(bots.Options{Seed: 5}, Hosted())
	if err != nil {
		t.Fatalf("New bench-style: %v", err)
	}
	if got := bench.(bots.BudgetedSeat).DecisionBudgetMS(); got != 0 {
		t.Fatalf("DecisionBudgetMS = %d, want 0 (bench and training stay unbounded)", got)
	}
}
