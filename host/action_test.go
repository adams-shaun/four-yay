package host

import (
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// oneSeatHuman is an Options.Seats hook: a single HumanSeat. A one-seat
// table is over as soon as it starts (the lone player wins) without the
// human ever answering a decision, so a perpetual one-seat table cycles
// through matched finishes with the run goroutine alive throughout.
func oneSeatHuman(names []string, seed uint64) []seat.Seat {
	return []seat.Seat{NewHumanSeat()}
}

// twoSeatHumans is an Options.Seats hook: a HumanSeat at slot 0 and a real
// bot at slot 1, so a table parks on the human while the bot plays its side.
func twoSeatHumans(names []string, seed uint64) []seat.Seat {
	return []seat.Seat{NewHumanSeat(), seat.NewBot(seed ^ 1)}
}

// startHumanTable returns a registry with a started 2-seat table whose seat
// 0 is human and seat 1 is a bot. The registry is closed on cleanup.
func startHumanTable(t *testing.T) (*Registry, TableID) {
	t.Helper()
	o := testOptions(t)
	o.Seats = twoSeatHumans
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := TableConfig{ID: "t1", Name: "human", Seats: 2, Decks: []string{"a", "b"},
		Seed: 42, Pace: 0, Spectator: view.Omniscient, Perpetual: false}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	return r, "t1"
}

// legalIntent answers d with the first Min options: always valid per
// Decision.Validate for any Kind, so the test needs no rules knowledge and
// can answer whatever decision the engine actually asks the human.
func legalIntent(d *decision.Decision) decision.Intent {
	n := d.Min
	if n > len(d.Options) {
		n = len(d.Options)
	}
	ch := make([]int, 0, n)
	for i := 0; i < n; i++ {
		ch = append(ch, d.Options[i].Index)
	}
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}
}

// waitPending polls Pending until it returns a decision (the seat has
// parked) or fails the test on timeout. Only for polls that PRECEDE any
// answer on that seat: inside the stale-ask window between SubmitIntent's
// acceptance and the match goroutine clearing the slot, a first-200 poll
// re-serves the just-answered ask — after an answer, use waitNextPending
// and wait for a different Seq.
func waitPending(t *testing.T, r *Registry, id TableID, k int, p state.PlayerID) *decision.Decision {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		d, err := r.Pending(id, k, p)
		if err == nil {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("no pending decision for player %d within deadline: %v", p, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPendingThenSubmitIntentAdvancesTheGame(t *testing.T) {
	t.Parallel()
	r, id := startHumanTable(t)
	d1 := waitPending(t, r, id, 1, 0)
	if err := r.SubmitIntent(id, 1, 0, legalIntent(d1)); err != nil {
		t.Fatalf("SubmitIntent(legal): %v", err)
	}
	// The game must move off d1: poll until a pending decision with a new
	// Seq appears (the seat is re-asked, possibly after the bot acts).
	deadline := time.Now().Add(20 * time.Second)
	for {
		d2, err := r.Pending(id, 1, 0)
		if err == nil && d2.Seq != d1.Seq {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("game did not advance after SubmitIntent; last err=%v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestSubmitIntentRejectsStaleIntentWithoutMovingTheGame(t *testing.T) {
	t.Parallel()
	r, id := startHumanTable(t)
	d1 := waitPending(t, r, id, 1, 0)
	stale := decision.Intent{Seq: d1.Seq + 1, Player: d1.Player, Choices: []int{0}}
	if err := r.SubmitIntent(id, 1, 0, stale); err == nil {
		t.Fatal("SubmitIntent accepted a stale Seq")
	}
	// A rejected intent must leave the seat parked on the same decision: the
	// game did not move.
	for i := 0; i < 5; i++ {
		d2, err := r.Pending(id, 1, 0)
		if err != nil || d2.Seq != d1.Seq {
			t.Fatalf("game moved or seat unparked after a rejected stale intent: err=%v d=%+v", err, d2)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestPendingResolutionErrors(t *testing.T) {
	t.Parallel()
	r, id := startHumanTable(t)
	waitPending(t, r, id, 1, 0) // sync: match live, seek slots installed

	if _, err := r.Pending("ghost", 1, 0); err == nil {
		t.Error("Pending on a missing table succeeded")
	}
	if err := r.SubmitIntent("ghost", 1, 0, decision.Intent{}); err == nil {
		t.Error("SubmitIntent on a missing table succeeded")
	}
	if _, err := r.Pending(id, 1, 5); err == nil {
		t.Error("Pending with an out-of-range player succeeded")
	}
	if _, err := r.Pending(id, 1, 1); err == nil {
		t.Error("Pending for a bot seat succeeded")
	}
}

// TestPendingTakesNonLiveBranchWhileMatchGoroutineAlive is the race-relevant
// counterpart to TestPendingOnAFinishedMatchErrors: it reaches the non-live
// branch of humanSeat (match not MatchLive) WITHOUT waiting for the match
// goroutine to exit first. A single-seat perpetual table finishes on its own
// in microseconds (a one-player game is over as soon as it begins, so the
// human seat is never even asked), then parks its run goroutine in the
// cooldown sleep before starting the next match. The table is perpetual, so
// that goroutine stays alive for the whole test (it only exits on Close):
// we take the branch while m.state is MatchFinished and the goroutine is
// still up — exactly the window a reader had previously raced by touching
// m.state without the lock. Reading on while the goroutine goes on to play
// match 2 proves it was alive when we broke out.
func TestPendingTakesNonLiveBranchWhileMatchGoroutineAlive(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	o.Sleep = defaultSleep              // real sleep so the perpetual cooldown parks the goroutine
	o.Cooldown = 500 * time.Millisecond // generous, race-visible window
	o.Seats = oneSeatHuman
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := TableConfig{ID: "t1", Name: "alive", Seats: 1, Decks: []string{"a"},
		Seed: 7, Pace: 0, Spectator: view.Public, Perpetual: true}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	// Poll Pending on match 1's human seat (never call Wait — the point is
	// to hit the non-live branch while the match goroutine is still running)
	// until it returns the non-live "nothing pending" error. Anything else
	// is a transient of the match coming to life (not-found / not parked yet)
	// or an early slots read, so retry it.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("match 1 never reached a non-live state within deadline")
		}
		_, perr := r.Pending("t1", 1, 0)
		if perr == nil {
			continue
		}
		if !strings.Contains(perr.Error(), "nothing pending") {
			continue
		}
		break // took the non-live branch on match 1, goroutine still alive
	}

	ms, err := r.Matches("t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].State != protocol.MatchFinished {
		t.Fatalf("expected a finished match at k=1, got %d matches: %+v", len(ms), ms)
	}

	// Aliveness: the perpetual run goroutine must still be running — it goes
	// on to play match 2, which also finishes instantly. If the goroutine had
	// exited, match 2 would never exist. This proves we observed match 1's
	// non-live state while the match goroutine was alive.
	reached := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(reached) {
			t.Fatal("match goroutine did not advance to match 2; it was not alive")
		}
		_, err := r.Pending("t1", 2, 0)
		if err != nil && strings.Contains(err.Error(), "nothing pending") {
			break // match 2 finished too: the goroutine drove the engine past match 1
		}
	}
}

func TestPendingOnAFinishedMatchErrors(t *testing.T) {
	t.Parallel()
	// An all-bot 2-seat table finishes on its own; answering it is meaningless.
	o := testOptions(t)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := TableConfig{ID: "t1", Name: "done", Seats: 2, Decks: []string{"a", "b"},
		Seed: 7, Pace: 0, Spectator: view.Public, Perpetual: false}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	if _, err := r.Pending("t1", 1, 0); err == nil {
		t.Error("Pending on a finished match succeeded")
	}
	if err := r.SubmitIntent("t1", 1, 0, decision.Intent{}); err == nil {
		t.Error("SubmitIntent on a finished match succeeded")
	}
}
