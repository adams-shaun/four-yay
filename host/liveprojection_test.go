package host

// Regression for the live-projection race: a focus client joining, or a
// match starting/rewinding while clients watch, could crash its table with
// an intermittent module-gate panic in rules.(*Engine).Derived /
// FilterDerivedPT. view.ProjectFor MUTATES the engine while it builds a
// View (rules/layers.go appends to e.derivedPTFrames and defers a
// truncation, writes derivedDepth/scratch, and reuses the active()
// continuous-effect cache), so two projections of the same live engine
// running concurrently corrupt each other's in-progress layer-7 P/T frames.
//
// The three live-projection sites — Subscribe (host/session.go),
// onMatchStart and pushRewind (host/fanout.go, host/undo.go) — used to hold
// only m.mu.RLock while calling snapshotFrame/snapshotBody, which is a read
// lock over a write. They now go through Registry.projectLive, which takes
// m.mu EXCLUSIVELY.
//
// The test is lock-sensitive, not probabilistic, in the failure direction
// that matters: it holds m.mu.RLock (the lock the old projection used)
// BEFORE launching each production entry point, so a correctly exclusive
// projection can never complete while the read lock is held — there is no
// window in which it could. With the fix reverted the entry point's own
// RLock is compatible, it completes, and the pinned assertion fails. The
// test also validates the payload and ordering each entry point delivers
// (a snapshot/rewind that decodes, matches the single-threaded baseline
// board including derived P/T, and arrives in the right order), and runs
// overlapping focus subscribes against start/rewind so a broken payload or
// a reordered delivery fails too.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/state"
)

// liveProjectionBoard plays fourSeatTable("t1", false) to completion once,
// publishes its finished match as the table's live match (so Subscribe's
// focus branch has something to project), and returns the registry, the
// table and that match. A finished match still holds a real, populated
// engine — the exact object snapshotBody projects — while the match
// goroutine is gone, so the only concurrent writers to the engine are this
// test's own projections.
func liveProjectionBoard(t *testing.T) (*Registry, *table, *match) {
	t.Helper()
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	r.Wait("t1")
	r.mu.RLock()
	tb := r.tables["t1"]
	r.mu.RUnlock()
	tb.mu.RLock()
	m := tb.history[0]
	tb.mu.RUnlock()
	if m == nil {
		t.Fatal("finished table recorded no match")
	}
	// t.cur is normally cleared once a match finishes; publish it so
	// Subscribe's focus branch projects a live engine.
	tb.mu.Lock()
	tb.cur = m
	tb.mu.Unlock()
	return r, tb, m
}

// assertSnapshotMatches is the payload half of the regression: a frame a
// live-projection entry point delivered must decode to the SAME board the
// single-threaded baseline projection built — head, seat roster, turn starts
// and the WHOLE wire view, including the layer-7 derived power/toughness. It
// compares the marshalled JSON, not reflect.DeepEqual: the projection's
// contract is the bytes a client receives, and DeepEqual would also (and
// spuriously) fail on Go-level nil-vs-empty slice distinctions that never
// reach the wire.
func assertSnapshotMatches(t *testing.T, label string, got protocol.Snapshot, base protocol.Snapshot) {
	t.Helper()
	if got.Head != base.Head {
		t.Fatalf("%s: snapshot head %d differs from baseline %d", label, got.Head, base.Head)
	}
	gj, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s: marshal delivered snapshot: %v", label, err)
	}
	bj, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("%s: marshal baseline snapshot: %v", label, err)
	}
	if !bytes.Equal(gj, bj) {
		t.Fatalf("%s: projected snapshot differs from the single-threaded baseline "+
			"(a concurrent projection corrupted the shared derived-P/T state)\n got=%s\nbase=%s", label, gj, bj)
	}
}

// assertLiveProjectionPinned holds m.mu.RLock BEFORE launching run and
// asserts run does not complete while it is held, then releases the lock and
// asserts run completes within a bound. It returns after run has finished.
//
// Locking first is what makes the assertion sound rather than a timing
// guess: a correctly exclusive run (projectLive's m.mu.Lock) can NEVER
// satisfy the read lock this test holds, so it cannot complete early no
// matter how the scheduler orders it — only a broken run that takes
// m.mu.RLock can. The bounded Gosched spin after the started handshake only
// reduces the chance of missing a broken build (the read lock is compatible
// with the old discipline, so a broken run may still be descheduled before
// it reaches its own RLock); it can never fail a correct one.
func assertLiveProjectionPinned(t *testing.T, m *match, label string, run func()) {
	t.Helper()
	m.mu.RLock()
	started := make(chan struct{})
	resumed := make(chan struct{})
	go func() {
		close(started)
		run()
		close(resumed)
	}()
	<-started
	for i := 0; i < 2000; i++ {
		runtime.Gosched() // give run every chance to (wrongly) pass the projection lock
	}
	select {
	case <-resumed:
		m.mu.RUnlock()
		t.Fatalf("precondition failed: %s completed while m.mu was pinned by a reader; a live projection must take m.mu exclusively", label)
	default:
	}
	m.mu.RUnlock()
	select {
	case <-resumed:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not complete after m.mu was released — deadlock", label)
	}
}

// TestConcurrentLiveSnapshotsSerializeEngineProjection proves every live
// engine projection takes exclusive m.mu, and that concurrent projections
// produce valid, uncorrupted snapshots in the right order.
func TestConcurrentLiveSnapshotsSerializeEngineProjection(t *testing.T) {
	t.Parallel()
	r, tb, m := liveProjectionBoard(t)

	// Precondition: the board is real and the view derives characteristics
	// for it. Without objects on the battlefield the projection would have
	// nothing whose layer-7 frames could be corrupted, and this test would
	// pass vacuously.
	board := 0
	for _, p := range m.e.G.Players {
		board += len(m.e.G.Zone(state.ZBattlefield, p.ID))
	}
	if board == 0 {
		t.Fatalf("precondition: finished match has no battlefield objects to project")
	}
	// A live projection in isolation succeeds and carries derived P/T on at
	// least one permanent, so the frames the race would corrupt are actually
	// built. This also pins the deterministic (single-threaded) result every
	// concurrent projection below must reproduce byte for byte.
	var base protocol.Snapshot
	r.projectLive(m, func() { base = r.snapshotBody(tb, m) })
	derivedSeen := 0
	for _, p := range base.View.Players {
		for _, c := range p.Battlefield {
			if c.Power != 0 || c.Toughness != 0 {
				derivedSeen++
			}
		}
	}
	if derivedSeen == 0 {
		t.Fatalf("precondition: projected board has no permanent with derived power/toughness")
	}

	// A standing focus subscriber must exist for onMatchStart/pushRewind to
	// build a snapshot at all (they return early with no sessions); Subscribe
	// also exercises the snapshot path itself.
	standing := r.OpenSession()
	if err := r.Subscribe(standing, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	if f := <-standing.Out(); f.T != protocol.TSnapshot {
		t.Fatalf("focus Subscribe delivered %s first, want TSnapshot", f.T)
	}

	// Phase A: Subscribe's own live projection. The frame it delivers must
	// arrive and must match the baseline board.
	var subFrame protocol.Frame
	assertLiveProjectionPinned(t, m, "Subscribe", func() {
		s := r.OpenSession()
		if err := r.Subscribe(s, "t1", protocol.ModeFocus); err != nil {
			t.Errorf("Subscribe: %v", err)
			return
		}
		select {
		case f := <-s.Out():
			subFrame = f
		case <-time.After(10 * time.Second):
			t.Errorf("concurrent Subscribe delivered no snapshot")
		}
	})
	if subFrame.T != protocol.TSnapshot {
		t.Fatalf("Subscribe delivered %s, want TSnapshot", subFrame.T)
	}
	assertSnapshotMatches(t, "Subscribe snapshot", decode[protocol.Snapshot](t, subFrame), base)

	// Phase B: match-start's live projection. A fresh focus subscriber gets
	// the subscribe snapshot first; onMatchStart then owes it a
	// TMatchStart frame followed by a TSnapshot of the same board, IN THAT
	// ORDER.
	sb := r.OpenSession()
	if err := r.Subscribe(sb, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	if f := <-sb.Out(); f.T != protocol.TSnapshot {
		t.Fatalf("phase-B subscribe delivered %s first, want TSnapshot", f.T)
	}
	assertLiveProjectionPinned(t, m, "onMatchStart", func() { r.onMatchStart(tb, m) })
	startFrames := drainNow(sb)
	if len(startFrames) != 2 || startFrames[0].T != protocol.TMatchStart || startFrames[1].T != protocol.TSnapshot {
		t.Fatalf("onMatchStart delivered %v, want [TMatchStart TSnapshot] in that order", frameTypes(startFrames))
	}
	assertSnapshotMatches(t, "onMatchStart snapshot", decode[protocol.Snapshot](t, startFrames[1]), base)

	// Phase C: rewind's live projection. A fresh focus subscriber gets the
	// subscribe snapshot first; pushRewind then owes it a TRewind frame
	// carrying the full board, before any decision frame.
	sc := r.OpenSession()
	if err := r.Subscribe(sc, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	if f := <-sc.Out(); f.T != protocol.TSnapshot {
		t.Fatalf("phase-C subscribe delivered %s first, want TSnapshot", f.T)
	}
	assertLiveProjectionPinned(t, m, "pushRewind", func() { r.pushRewind(tb, m) })
	rewindFrames := drainNow(sc)
	if len(rewindFrames) == 0 || rewindFrames[0].T != protocol.TRewind {
		t.Fatalf("pushRewind delivered %v, want a TRewind first", frameTypes(rewindFrames))
	}
	for i, f := range rewindFrames {
		if i == 0 {
			continue
		}
		if f.T == protocol.TRewind {
			t.Fatalf("pushRewind delivered a second TRewind at index %d", i)
		}
	}
	assertSnapshotMatches(t, "pushRewind snapshot", decode[protocol.Snapshot](t, rewindFrames[0]), base)

	// Phase D: an overlapping focus Subscribe and a pushRewind must BOTH
	// block on the exclusive projection lock and then both deliver a valid
	// board — the competing-projection case the race crashed on. (Subscribe
	// holds t.fanMu while it waits for m.mu here; pushRewind drops m.mu
	// before t.fanMu, so this also exercises the fanMu -> m.mu order under
	// contention rather than deadlocking.)
	sd := r.OpenSession()
	if err := r.Subscribe(sd, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	if f := <-sd.Out(); f.T != protocol.TSnapshot {
		t.Fatalf("phase-D subscribe delivered %s first, want TSnapshot", f.T)
	}
	se := r.OpenSession()
	m.mu.RLock()
	dStarted := make(chan struct{}, 2)
	dSubDone := make(chan struct{}, 1)
	dRewindDone := make(chan struct{}, 1)
	go func() {
		dStarted <- struct{}{}
		if err := r.Subscribe(se, "t1", protocol.ModeFocus); err != nil {
			t.Errorf("overlapping Subscribe: %v", err)
		}
		dSubDone <- struct{}{}
	}()
	go func() {
		dStarted <- struct{}{}
		// pushRewind delivers to every focus subscriber; sd is the one whose
		// stream this phase reads (fresh and otherwise quiescent).
		r.pushRewind(tb, m)
		dRewindDone <- struct{}{}
	}()
	<-dStarted
	<-dStarted
	for i := 0; i < 2000; i++ {
		runtime.Gosched()
	}
	select {
	case <-dSubDone:
		m.mu.RUnlock()
		t.Fatalf("overlapping Subscribe completed while m.mu was pinned by a reader")
	default:
	}
	select {
	case <-dRewindDone:
		m.mu.RUnlock()
		t.Fatalf("overlapping pushRewind completed while m.mu was pinned by a reader")
	default:
	}
	m.mu.RUnlock()
	select {
	case <-dSubDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("overlapping Subscribe did not complete after m.mu was released — deadlock")
	}
	select {
	case <-dRewindDone:
	case <-time.After(10 * time.Second):
		t.Fatalf("overlapping pushRewind did not complete after m.mu was released — deadlock")
	}
	// Subscribe registers se.subs BEFORE it takes t.fanMu, so pushRewind's
	// sessionsFor can already include se and win the exclusive projection
	// lock: se's FIRST frame may legitimately be a TRewind rather than its
	// own TSnapshot (and in the other schedule, [TSnapshot TRewind]). Both
	// are correct deliveries, so scan the whole stream for the frames this
	// phase owes instead of assuming an order the scheduling does not
	// guarantee — an ordering assertion here would fail a healthy build.
	subFrames := drainNow(se)
	var subSnap, subRewind *protocol.Frame
	for i := range subFrames {
		switch subFrames[i].T {
		case protocol.TSnapshot:
			if subSnap == nil {
				subSnap = &subFrames[i]
			}
		case protocol.TRewind:
			if subRewind == nil {
				subRewind = &subFrames[i]
			}
		}
	}
	if subSnap == nil {
		t.Fatalf("overlapping Subscribe delivered %v, want a TSnapshot", frameTypes(subFrames))
	}
	assertSnapshotMatches(t, "overlapping Subscribe snapshot", decode[protocol.Snapshot](t, *subSnap), base)
	if subRewind != nil {
		// If the competing rewind beat Subscribe to se, that rewind still
		// owes the same uncorrupted board.
		assertSnapshotMatches(t, "overlapping Subscribe rewind", decode[protocol.Snapshot](t, *subRewind), base)
	}
	sdFrames := drainNow(sd)
	var sdRewind *protocol.Frame
	for i := range sdFrames {
		if sdFrames[i].T == protocol.TRewind {
			sdRewind = &sdFrames[i]
			break
		}
	}
	if sdRewind == nil {
		t.Fatalf("overlapping pushRewind delivered %v, want a TRewind", frameTypes(sdFrames))
	}
	assertSnapshotMatches(t, "overlapping pushRewind snapshot", decode[protocol.Snapshot](t, *sdRewind), base)

	// Phase E: many exclusive live projections at once must all produce a
	// snapshot byte-identical to the single-threaded baseline — including the
	// derived power/toughness the race corrupts. A panicked goroutine would
	// crash the test binary; a divergent board fails on the comparison.
	baseJSON, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	const n = 32
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			var snap protocol.Snapshot
			var panicked any
			func() {
				defer func() { panicked = recover() }()
				r.projectLive(m, func() { snap = r.snapshotBody(tb, m) })
			}()
			if panicked != nil {
				errs <- fmt.Errorf("storm projection panicked: %v", panicked)
				return
			}
			if snap.Head != base.Head {
				errs <- fmt.Errorf("storm snapshot head %d differs from baseline %d", snap.Head, base.Head)
				return
			}
			sj, err := json.Marshal(snap)
			if err != nil {
				errs <- fmt.Errorf("storm snapshot marshal: %v", err)
				return
			}
			if !bytes.Equal(sj, baseJSON) {
				errs <- fmt.Errorf("storm snapshot differs from the single-threaded baseline: %s", sj)
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent live projection: %v", err)
		}
	}

	// The match mutex is not left held after the storm.
	if !m.mu.TryLock() {
		t.Fatal("m.mu still held after every live projection returned")
	}
	m.mu.Unlock()
}

// frameTypes names the frame types in order, for a readable ordering failure.
func frameTypes(fs []protocol.Frame) []protocol.FrameType {
	ts := make([]protocol.FrameType, len(fs))
	for i, f := range fs {
		ts[i] = f.T
	}
	return ts
}
