package mzplay

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/internal/mzbridge"
)

// TestPlayGameUpstreamSearch: with every upstream switch on
// (SetUpstreamSearch) a game plays to its end reproducibly; attack and block
// declarations are searched one creature at a time, each creature's search
// recorded as its own yes/no or which-attacker row; the records stay well
// formed (a root's recorded visits never exceed the budget; the score is
// upstream's root mean, clamped into [-1, 1]).
func TestPlayGameUpstreamSearch(t *testing.T) {
	gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 12)
	gs.MaxTurns = 10
	for i := range gs.Seats {
		gs.Seats[i].SetUpstreamSearch()
	}
	a, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	b, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same setup with the upstream switches played two different games")
	}
	st := a.Stats
	if st.CombatSteps == 0 || st.RowsUse == 0 || st.OppSelections == 0 || st.ReuseHits == 0 || st.Searches < 10 {
		t.Fatalf("upstream search: %+v", st)
	}
	if st.SimFailures > st.Simulations/10 {
		t.Fatalf("%d of %d simulations failed", st.SimFailures, st.Simulations)
	}
	if st.RowsPriority+st.RowsTarget+st.RowsUse != len(a.Rows[0])+len(a.Rows[1]) {
		t.Fatalf("row counts %d+%d+%d != %d+%d", st.RowsPriority, st.RowsTarget, st.RowsUse, len(a.Rows[0]), len(a.Rows[1]))
	}
	for seat, rows := range a.Rows {
		budget := gs.Seats[seat].Budget
		for i, r := range rows {
			sum := float32(0)
			for _, p := range r.Policy {
				if r.Type == mzbridge.ChooseUse && p.Index > 1 {
					t.Fatalf("seat %d row %d: a yes/no record with index %d", seat, i, p.Index)
				}
				sum += p.Visits
			}
			if sum < 1 || sum > float32(budget) {
				t.Fatalf("seat %d row %d: policy counts sum to %v with a budget of %d", seat, i, sum, budget)
			}
			if r.Score < -1 || r.Score > 1 || r.Value < -1 || r.Value > 1 {
				t.Fatalf("seat %d row %d: score %v value %v", seat, i, r.Score, r.Value)
			}
		}
	}
	gs.Seats[0] = testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 12).Seats[0]
	gs.Seats[1] = testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 12).Seats[1]
	off, err := PlayGame(gs)
	if err != nil {
		t.Fatal(err)
	}
	if off.Stats.CombatSteps != 0 || reflect.DeepEqual(off.Rows, a.Rows) {
		t.Fatalf("switch off: %d creature searches, rows equal %v", off.Stats.CombatSteps, reflect.DeepEqual(off.Rows, a.Rows))
	}
	t.Logf("turns %d: %d searches (%d creature steps), %d bot (%v), %d simulations (%d failed); rows priority %d target %d use %d; reuse %d hits, %d carried; %d opponent selections",
		a.Turns, st.Searches, st.CombatSteps, st.Bot, st.BotByKind, st.Simulations, st.SimFailures, st.RowsPriority, st.RowsTarget, st.RowsUse, st.ReuseHits, st.ReuseCarried, st.OppSelections)
}

// countdownCtx is a decision context whose deadline passes after n checks:
// the search stops after exactly n simulations, with no wall clock.
type countdownCtx struct {
	n     int64
	calls atomic.Int64
}

func (c *countdownCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *countdownCtx) Done() <-chan struct{}       { return nil }
func (c *countdownCtx) Value(any) any               { return nil }
func (c *countdownCtx) Err() error {
	if c.calls.Add(1) > c.n {
		return context.DeadlineExceeded
	}
	return nil
}

// TestPlayGameDeadlineBestChild: a decision deadline that stops every search
// part-way plays and records the partial tree's best child under
// DeadlineBestChild (upstream's searchTimeout), and the bot's answer, with
// no record, without it.
func TestPlayGameDeadlineBestChild(t *testing.T) {
	for _, best := range []bool{false, true} {
		gs := testSetup(t, "mono-green-stompy", "mono-white-equipment", 7, 30)
		gs.MaxTurns = 6
		gs.DecisionContext = func(int) (context.Context, context.CancelFunc) {
			return &countdownCtx{n: 5}, func() {}
		}
		for i := range gs.Seats {
			gs.Seats[i].DeadlineBestChild = best
		}
		r, err := PlayGame(gs)
		if err != nil {
			t.Fatal(err)
		}
		st := r.Stats
		if st.Timeouts == 0 {
			t.Fatalf("best %v: no search hit the deadline: %+v", best, st)
		}
		rows := len(r.Rows[0]) + len(r.Rows[1])
		if best && (st.Searches == 0 || rows == 0) || !best && (st.Searches != 0 || rows != 0) {
			t.Fatalf("best %v: %d searches answered from the tree, %d rows (%d timeouts)", best, st.Searches, rows, st.Timeouts)
		}
		t.Logf("best %v: %d timeouts, %d searches, %d bot, %d rows", best, st.Timeouts, st.Searches, st.Bot, rows)
	}
}

// mcts.upstream_search is read per seat and defaults to off.
func TestParseConfigUpstreamSearch(t *testing.T) {
	src := "player_a:\n  type: mcts\n  mcts:\n    upstream_search: true\nplayer_b:\n  type: mcts\ntraining:\n  games: 1\nserver:\n  port: 1\n"
	c, err := ParseConfig(src)
	if err != nil {
		t.Fatal(err)
	}
	if !c.A.UpstreamSearch || c.B.UpstreamSearch {
		t.Fatalf("upstream_search: a %v b %v", c.A.UpstreamSearch, c.B.UpstreamSearch)
	}
	var s SeatSetup
	s.SetUpstreamSearch()
	if !s.OpponentNodes || !s.ReuseTree || !s.ParentVisits || !s.DeadlineBestChild || !s.CombatSteps || !s.MicroKinds {
		t.Fatalf("SetUpstreamSearch left a switch off: %+v", s)
	}
}
