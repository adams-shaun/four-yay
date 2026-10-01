package host

import (
	"testing"

	"github.com/adams-shaun/gorge/bots"
	"github.com/adams-shaun/gorge/view"
)

// TestDeterminismGateBuildsUnboundedSearchSeats pins the wiring behind
// TestEveryHostedPolicyIsDeterministic: with Options.BotUnboundedDecisions
// set, every table bot the registry's play path builds declares a zero
// per-decision budget (bots.BudgetedSeat.DecisionBudgetMS() == 0,
// decisionCtx's unbounded shape). The determinism gate compares two runs of
// one seed byte-for-byte; arming the hosted wall-clock budget under it makes
// a deadline bail-out answer from the non-searched fallback in one run and
// not the other, which is load, not seed (BP-07 §7 rejected a wall-clock
// budget for exactly this reason). Regression pin: with the knob absent or
// the gate not setting it, the seat declares hostedDecisionDeadlineMS and
// this test fails.
//
// The served arm below asserts the precondition the zero-budget assertion
// depends on: the az-redeal policy really is a budgeted search seat that
// declares hostedDecisionDeadlineMS when the knob is off, so a reverted knob
// cannot satisfy the unbounded arm by accident, and the seats it inspects are
// the real ones play built.
func TestDeterminismGateBuildsUnboundedSearchSeats(t *testing.T) {
	const seed = uint64(20260928)
	cfg := TableConfig{ID: "t1", Seats: 2, Decks: []string{"a", "b"}, Seed: seed, Pace: 0, Spectator: view.Public, BotPolicy: azRedealPolicy}

	slotsOf := func(t *testing.T, unbounded bool) {
		t.Helper()
		opts := testOptions(t)
		// Two intents end the match right after play has built and parked
		// the seats; the assertion is about seat construction, not play.
		opts.MaxIntents = 2
		if unbounded {
			opts.BotUnboundedDecisions = true
		}
		r, err := New(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if err := r.AddTable(cfg); err != nil {
			t.Fatalf("AddTable: %v", err)
		}
		if err := r.Start("t1"); err != nil {
			t.Fatalf("Start: %v", err)
		}
		r.Wait("t1")
		r.mu.RLock()
		tbl := r.tables["t1"]
		r.mu.RUnlock()
		tbl.mu.RLock()
		if len(tbl.history) != 1 {
			t.Fatalf("table kept %d matches, want 1", len(tbl.history))
		}
		m := tbl.history[0]
		tbl.mu.RUnlock()
		m.mu.RLock()
		slots := m.slots
		m.mu.RUnlock()
		if len(slots) != cfg.Seats {
			t.Fatalf("play built %d seats, want %d (unbounded=%v)", len(slots), cfg.Seats, unbounded)
		}
		want := 0
		if !unbounded {
			// Precondition: the served posture arms the hosted budget, so the
			// unbounded arm's zero assertion is not vacuously satisfiable.
			want = hostedDecisionDeadlineMS
		}
		for i, s := range slots {
			b, ok := s.(bots.BudgetedSeat)
			if !ok {
				t.Fatalf("seat %d (%T) does not implement bots.BudgetedSeat; the budget assertion would be vacuous", i, s)
			}
			if got := b.DecisionBudgetMS(); got != want {
				t.Fatalf("seat %d declares budget %d ms, want %d (unbounded=%v)", i, got, want, unbounded)
			}
		}
	}

	slotsOf(t, true)  // the gate's posture: unbounded
	slotsOf(t, false) // the served posture: hostedDecisionDeadlineMS
}
