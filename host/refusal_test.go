package host

// BP-06: the host refusal ladder (spec 2026-09-28-hosted-bot-packages §5.5).
// A seat that implements bots.RefusalAnswerer gets one retry and then the
// shared bots.Fallbacks rungs before the crash; every other seat keeps the
// old crash, unchanged. These tests drive a real 4-seat match through
// Options.Seats with a wrapper that answers one decision with an intent no
// engine can accept (an option index past the offered list: a
// Decision.Validate rejection, so Submit returns before recording anything
// and the pending decision survives for the ladder's next rung -- the same
// preserve-and-reject shape the spellbench menace case relies on, though the
// menace case is refused later, by rules.validateBlockers, rather than by
// Validate). The kind of decision does not matter to the ladder, only that
// the first Submit is refused; the wrapper makes no assumption about which
// kind decision ordinal 1 is. They then read the resulting sidecar
// counters.

import (
	"context"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// refusableIntent is an answer the engine's Decision.Validate rejects: an
// option index that was never offered.
func refusableIntent(d decision.Decision) decision.Intent {
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{len(d.Options) + 7}}
}

// refusingSeat wraps a real bot and answers one shared decision ordinal with
// a refusable intent. It implements bots.RefusalAnswerer, so the host runs
// the BP-06 ladder for it. retryValid selects the ladder's branch: true
// returns the wrapped bot's own answer (rung 1 accepts), false returns a
// second refusable intent so the ladder falls through to bots.Fallbacks.
type refusingSeat struct {
	inner      seat.Seat
	retryValid bool
	n          *int // shared across seats: the next decision ordinal
	at         int  // the ordinal that is answered refusably
	refusals   *int // shared count of Decide answers refused
	retries    *int // shared count of AnswerRefused calls
}

func (s *refusingSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	*s.n++
	if *s.n == s.at {
		*s.refusals++
		return refusableIntent(d), nil
	}
	return s.inner.Decide(ctx, v, d)
}

func (s *refusingSeat) AnswerRefused(v view.View, d decision.Decision, _ decision.Intent) decision.Intent {
	*s.retries++
	if !s.retryValid {
		return refusableIntent(d)
	}
	// The re-projected View is exactly what a plain Seat gets, so the wrapped
	// seat can answer afresh. A bot ignores ctx.
	in, err := s.inner.Decide(context.Background(), v, d)
	if err != nil {
		return refusableIntent(d)
	}
	return in
}

// refusalOptions builds a 4-seat match whose seats are all refusingSeat
// wrappers around real bots, with one shared decision ordinal and counters.
func refusalOptions(t *testing.T, retryValid bool) (Options, *int, *int, *int) {
	t.Helper()
	o := testOptions(t)
	n, refusals, retries := 0, 0, 0
	o.Seats = func(names []string, seed uint64) []seat.Seat {
		out := make([]seat.Seat, len(names))
		for i := range names {
			out[i] = &refusingSeat{
				inner:      seat.NewBot(seed ^ uint64(i+1)),
				retryValid: retryValid,
				n:          &n,
				at:         1,
				refusals:   &refusals,
				retries:    &retries,
			}
		}
		return out
	}
	return o, &n, &refusals, &retries
}

// TestRefusalLadderAcceptsTheSeatRetry: the seat's first answer is refused,
// AnswerRefused is called exactly once, rung 1 is accepted, and the match
// finishes normally -- no fallback rung ran.
func TestRefusalLadderAcceptsTheSeatRetry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	o, _, refusals, retries := refusalOptions(t, true)
	o.Dir = dir
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	if *refusals != 1 {
		t.Fatalf("engine refused %d answers, want exactly 1", *refusals)
	}
	if *retries != 1 {
		t.Fatalf("AnswerRefused called %d times, want exactly 1", *retries)
	}
	ms, _ := r.Matches("t1")
	if len(ms) != 1 || ms[0].State != protocol.MatchFinished {
		t.Fatalf("match did not finish through the seat retry: %+v", ms)
	}
	sc, err := readSidecar(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Refusals != 1 {
		t.Fatalf("sidecar refusals = %d, want 1", sc.Refusals)
	}
	if sc.Fallbacks != 0 {
		t.Fatalf("sidecar fallbacks = %d, want 0 (rung 1 accepted)", sc.Fallbacks)
	}
}

// TestRefusalLadderFallsBackToMinimalThenBot: both the seat's answer and its
// retry are refused, so the ladder reaches the shared fallbacks in order.
// The minimal rung accepts here (a priority/mulligan empty answer is legal),
// which is what keeps the match alive; the bot rung's exact equality to the
// spellbench runner is pinned by bots.TestFallbacksMatchTheSpellbenchRunner.
func TestRefusalLadderFallsBackToMinimalThenBot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	o, _, refusals, retries := refusalOptions(t, false)
	o.Dir = dir
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	if *refusals != 1 {
		t.Fatalf("engine refused %d answers, want exactly 1", *refusals)
	}
	if *retries != 1 {
		t.Fatalf("AnswerRefused called %d times, want exactly 1", *retries)
	}
	ms, _ := r.Matches("t1")
	if len(ms) != 1 || ms[0].State != protocol.MatchFinished {
		t.Fatalf("match did not finish through the fallbacks: %+v", ms)
	}
	sc, err := readSidecar(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if sc.Refusals != 1 || sc.Fallbacks != 1 {
		t.Fatalf("sidecar refusals/fallbacks = %d/%d, want 1/1", sc.Refusals, sc.Fallbacks)
	}
	if strings.Contains(sc.Reason, "rejected") {
		t.Fatalf("match crashed instead of falling back: %q", sc.Reason)
	}
}

// plainRefusingSeat answers the first decision with a refusable intent and,
// crucially, does NOT implement bots.RefusalAnswerer. The host must keep the
// pre-BP-06 crash for it.
type plainRefusingSeat struct {
	inner seat.Seat
	n     *int
	at    int
}

func (s *plainRefusingSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	*s.n++
	if *s.n == s.at {
		return refusableIntent(d), nil
	}
	return s.inner.Decide(ctx, v, d)
}

// TestNonRefusalSeatStillCrashesOnRejection: a seat that is not a
// RefusalAnswerer gets no ladder and the match crashes with the rejected
// intent, exactly as before BP-06.
func TestNonRefusalSeatStillCrashesOnRejection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	o := testOptions(t)
	o.Dir = dir
	n := 0
	o.Seats = func(names []string, seed uint64) []seat.Seat {
		out := make([]seat.Seat, len(names))
		for i := range names {
			out[i] = &plainRefusingSeat{inner: seat.NewBot(seed ^ uint64(i+1)), n: &n, at: 1}
		}
		return out
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")

	if got := r.Tables()[0].State; got != protocol.TableHalted {
		t.Fatalf("table state %s, want TableHalted", got)
	}
	ms, _ := r.Matches("t1")
	if len(ms) != 1 || ms[0].State != protocol.MatchCrashed {
		t.Fatalf("matches %+v, want one crashed match", ms)
	}
	sc, err := readSidecar(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sc.Reason, "rejected") {
		t.Fatalf("crash reason %q does not name the rejected intent", sc.Reason)
	}
	if sc.Fallbacks != 0 {
		t.Fatalf("sidecar fallbacks = %d, want 0 (no ladder for a plain seat)", sc.Fallbacks)
	}
}
