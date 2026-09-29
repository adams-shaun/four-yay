package host

// BP-10 follow-up (search-slot-grant-beats-close): a table whose stop channel
// has closed must never enter DecideEnv while it is queued on a held search
// slot, even when the grant reaches it before its own run() watcher has
// cancelled the play context. Close closes every table's stop synchronously
// under r.mu; each run() cancels its play ctx asynchronously, so the granted
// waiter could otherwise observe a live ctx and answer one more searched
// decision before aborting at the next t.stop poll. acquire now observes
// t.stop directly (errTableClosed) and the play loop aborts on it, so the
// entry never happens. TestSearchSlotWaitAbortsOnClose pins the weaker
// holder-serialization invariant BP-13 kept; this pins the strict one.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/protocol"
)

// TestSearchSlotsAcquireObservesStop is the deterministic unit-level pin for
// the same rule: a waiter whose stop channel is closed but whose context is
// still live must NOT win the slot. It queues a waiter on a held slot, grants
// it by releasing the holder, closes the waiter's stop in the same breath,
// and asserts acquire reports errTableClosed and hands the slot on rather
// than returning nil on a live context. Since the waiter's ctx is a plain
// Background, the pre-fix code has no way to notice the stop and returns nil
// — so this fails without the fix on every run, unlike the ~1.5%/run
// integration race.
func TestSearchSlotsAcquireObservesStop(t *testing.T) {
	s := newSearchSlots(1)
	// The holder owns the only slot.
	if err := s.acquire(context.Background(), nil); err != nil {
		t.Fatalf("holder acquire: %v", err)
	}

	stop := make(chan struct{})
	errC := make(chan error, 1)
	go func() {
		// A live context: only the stop channel can end this wait.
		errC <- s.acquire(context.Background(), stop)
	}()

	// Precondition: the waiter is deterministically IN the queue behind the
	// holder before we touch the stop channel — a waiter that never queued
	// would make the assertion below vacuous.
	deadline := time.After(10 * time.Second)
	for {
		s.mu.Lock()
		queued := len(s.waiters)
		s.mu.Unlock()
		if queued == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("waiter never queued behind the held slot")
		case <-time.After(time.Millisecond):
		}
	}

	// Grant the waiter (release hands it the slot, held stays 1) and close
	// its stop in the same instant — the grant/stop race the fix closes.
	s.release()
	close(stop)

	select {
	case err := <-errC:
		if !errors.Is(err, errTableClosed) {
			t.Fatalf("acquire returned %v on a live context after stop closed; want errTableClosed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("acquire did not return after its stop closed")
	}

	// The slot must not have been swallowed: with no other waiter the pool
	// holds it again.
	s.mu.Lock()
	held := s.held
	s.mu.Unlock()
	if held != 0 {
		t.Fatalf("held counter %d after a stop-closed waiter returned, want 0 (the slot was swallowed)", held)
	}
}

func TestSearchSlotWaitNeverDecidesAfterClose(t *testing.T) {
	h := newBP10Harness(1)
	registerBP10Spy(t, h)
	opts := testOptions(t)
	opts.SearchSlots = 1
	opts.Seats = h.seatBuilder(t)
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	addAndStartBP10(t, r, bp10Tables(bp10SpyPolicy))

	// Table A's first search holds the only slot, blocked in DecideEnv.
	first := awaitBP10(t, h.enteredC, "table A's search decision to enter DecideEnv")
	// Table B reaches its projection, is queued behind A's slot...
	other := ""
	deadline := time.After(10 * time.Second)
	for other == "" || bp10Tag(other) == bp10Tag(first) {
		select {
		case w := <-h.wantC:
			if bp10Tag(w) != bp10Tag(first) {
				other = w
			}
		case <-deadline:
			t.Fatalf("table B never reached WantsEnv (entries: %v)", h.enteredLabels())
		}
	}
	// Precondition: B is deterministically IN the gate's queue and has NOT
	// entered DecideEnv (a B that never queued would fail loudly here, and a
	// B already entered would make the post-Close assertion vacuous).
	bp10WaitQueued(t, r, 1)
	select {
	case l := <-h.enteredC:
		t.Fatalf("table B entered DecideEnv while the slot was held: %s", l)
	default:
	}

	// Close closes both tables' stops; B's queued wait must observe its own
	// stop and hand the slot on rather than decide one more time.
	r.Close()
	done := make(chan struct{})
	go func() { r.Wait("s1"); r.Wait("s2"); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("tables did not finish after Close; a search-slot wait did not abort on cancellation")
	}

	// The strict invariant: B never entered DecideEnv at all. Without the
	// stop observation in acquire, the grant reaches B with a still-live ctx
	// and B decides once (the same window that made the old
	// TestSearchSlotWaitAbortsOnClose pin flaky).
	for _, l := range h.enteredLabels() {
		if bp10Tag(l) == bp10Tag(other) {
			t.Fatalf("table B entered DecideEnv (%s) despite its stop being closed; the queued table decided after Close", l)
		}
	}

	// Both matches are cleanly aborted, not crashed, and both tables went
	// idle (run()'s ordinary abort path), never halted. Mirrors
	// TestSearchSlotWaitAbortsOnClose's close tail.
	for _, id := range []TableID{"s1", "s2"} {
		for _, ti := range r.Tables() {
			if ti.ID == string(id) && ti.State != protocol.TableIdle {
				t.Fatalf("table %s state %s after Close, want idle (not halted)", id, ti.State)
			}
		}
		m, _ := matchFor(t, r, id)
		m.mu.RLock()
		state, reason := m.state, m.reason
		m.mu.RUnlock()
		if state != protocol.MatchAborted {
			t.Fatalf("table %s match state %q (reason %q), want aborted", id, state, reason)
		}
		if reason != "" {
			t.Fatalf("table %s aborted with a crash reason %q", id, reason)
		}
	}
}
