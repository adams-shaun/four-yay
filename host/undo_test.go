package host

// Undo (dispatch fb-20260911T201015Z): the in-place rewind tests. The v1
// attempt forked a successor match; its blocking MAJOR was that the fork
// pushed the whole inherited prefix through OnBurst as one burst (one call,
// many intents) — violating the one-intent-per-burst contract and breaking
// every embedder's persistence. This slice rewinds IN PLACE, and the
// regression test below is written against exactly that shape: after any
// undo, every OnBurst call still carries exactly one intent's burst.

import (
	"sync"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/protocol"
	"github.com/adams-shaun/gorge/replay"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// undoTable is a 2-seat table whose seat 0 is the table's only human seat
// (TableConfig.Humans drives play's seat replacement, so Options.Seats is
// left at the default bots): the shape an undo is allowed on.
func undoTable(t *testing.T, o Options, id TableID) *Registry {
	t.Helper()
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	cfg := TableConfig{ID: id, Name: "undo", Seats: 2, Decks: []string{"a", "b"},
		Seed: 42, Pace: 0, Spectator: view.Omniscient, Humans: []int{0}}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(id); err != nil {
		t.Fatal(err)
	}
	return r
}

// liveMatch returns the table's live match, failing the test if there is
// none within a deadline.
func liveMatch(t *testing.T, r *Registry, id TableID) *match {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		r.mu.RLock()
		tb := r.tables[id]
		r.mu.RUnlock()
		if tb != nil {
			tb.mu.RLock()
			m := tb.cur
			tb.mu.RUnlock()
			if m != nil {
				return m
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no live match on %s within deadline", id)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitIntents polls until the live match has recorded at least want intents.
func waitIntents(t *testing.T, r *Registry, id TableID, want int) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		m := liveMatch(t, r, id)
		m.mu.RLock()
		n := len(m.e.L.Intents)
		m.mu.RUnlock()
		if n >= want {
			return n
		}
		if time.Now().After(deadline) {
			t.Fatalf("match %s reached only %d intents, wanted %d", id, n, want)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitRewind waits for the hook that runs inside the rewind boundary. A
// count-based poll is not sufficient: bots may immediately rebuild a
// discarded tail and race the observer back to the old count.
func waitRewind(t *testing.T, rewound <-chan int) int {
	t.Helper()
	select {
	case n := <-rewound:
		return n
	case <-time.After(20 * time.Second):
		t.Fatal("rewind hook did not fire")
		return 0
	}
}

// answerOnce submits one legal intent for the human seat (the same
// first-Min-options answer action_test's legalIntent builds) and waits for
// the game to move past it. after must be the Seq of the decision answered
// immediately before on the same seat (0 for a seat's first ask): inside the
// stale-ask window between SubmitIntent's acceptance and the match goroutine
// clearing the slot, a first-200 poll re-serves the just-answered decision,
// so the initial poll must wait for a Seq past after.
func answerOnce(t *testing.T, r *Registry, id TableID, after uint64) *decision.Decision {
	t.Helper()
	d := waitNextPending(t, r, id, 1, 0, after)
	if err := r.SubmitIntent(id, 1, 0, legalIntent(d)); err != nil {
		t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		d2, err := r.Pending(id, 1, 0)
		if err == nil && d2.Seq != d.Seq {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("game did not advance after SubmitIntent(seq %d); last err=%v", d.Seq, err)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitPendingSeq(t *testing.T, r *Registry, id TableID, want uint64) *decision.Decision {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if d, err := r.Pending(id, 1, 0); err == nil && d.Seq == want {
			return d
		}
		if time.Now().After(deadline) {
			t.Fatalf("seat 0 did not become pending on seq %d", want)
		}
		time.Sleep(time.Millisecond)
	}
}

// driveHumanUntil answers each newly parked seat-0 decision until done fires.
// Seed 45's sample-deck match is a compact deterministic fixture whose final
// intent belongs to seat 0, which exposes the terminal undo boundary.
func driveHumanUntil(t *testing.T, r *Registry, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last uint64
	var answered bool
	for {
		select {
		case <-done:
			return
		default:
		}
		if d, err := r.Pending("t1", 1, 0); err == nil && (!answered || d.Seq != last) {
			if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
				t.Fatalf("SubmitIntent(seq %d): %v", d.Seq, err)
			}
			last, answered = d.Seq, true
		}
		if time.Now().After(deadline) {
			t.Fatal("human-driven match did not reach the terminal boundary")
		}
		time.Sleep(time.Millisecond)
	}
}

func addTerminalUndoTable(t *testing.T, r *Registry, pace time.Duration) {
	t.Helper()
	cfg := TableConfig{ID: "t1", Name: "terminal undo", Seats: 2, Decks: []string{"a", "b"},
		// Seed 45, not the original 55: under the CR 103.1 toss seed 55's match
		// ends on a seat-1 (bot) intent, and the watcher below needs the final
		// intent to be seat 0's. 45 is measured to keep the fixture's shape
		// (game ends, final intent seat 0's).
		Seed: 45, Pace: pace, Spectator: view.Omniscient, Humans: []int{0}}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
}

// TestUndoAfterGameEndingIntentWinsBeforeFinish covers the exact terminal
// window from the review: seat 0's final intent makes G.Over true, fanout runs,
// and the Undo is posted from inside the ensuing pace sleep. The accepted
// request must rewind the live match before any match_end is emitted.
func TestUndoAfterGameEndingIntentWinsBeforeFinish(t *testing.T) {
	const pace = 137 * time.Nanosecond
	o := testOptions(t)
	undoResult := make(chan error, 1)
	attempted := make(chan struct{})
	rewound := make(chan struct{})
	ended := make(chan struct{}, 1)
	var once sync.Once
	var r *Registry
	o.OnRewind = func(_ TableID, _ int, _ int, _ uint64) error {
		close(rewound)
		return nil
	}
	o.OnMatchEnd = func(_ TableID, _ int, _ protocol.MatchInfo) error {
		ended <- struct{}{}
		return nil
	}
	o.Sleep = func(d time.Duration, _ <-chan struct{}) {
		if d != pace || r == nil {
			return
		}
		r.mu.RLock()
		tb := r.tables["t1"]
		r.mu.RUnlock()
		if tb == nil {
			return
		}
		tb.mu.RLock()
		m := tb.cur
		tb.mu.RUnlock()
		if m == nil {
			return
		}
		m.mu.RLock()
		over := m.e.G.Over
		lastHuman := len(m.e.L.Intents) > 0 && m.e.L.Intents[len(m.e.L.Intents)-1].Player == 0
		m.mu.RUnlock()
		if over && lastHuman {
			once.Do(func() {
				undoResult <- r.Undo("t1", 1, 0)
				close(attempted)
			})
		}
	}
	var err error
	r, err = New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	addTerminalUndoTable(t, r, pace)

	s := r.OpenSession()
	t.Cleanup(func() { r.CloseSession(s.ID) })
	if err := r.Subscribe(s, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	// Drain throughout the match so the session's bounded ring cannot
	// overflow before the terminal frame whose ordering this test checks.
	streamTerminal := make(chan protocol.FrameType, 1)
	go func() {
		for f := range s.Out() {
			if f.T == protocol.TMatchEnd || f.T == protocol.TRewind {
				streamTerminal <- f.T
				return
			}
		}
	}()
	driveHumanUntil(t, r, attempted)
	if err := <-undoResult; err != nil {
		t.Fatalf("Undo in terminal pace sleep: %v", err)
	}

	select {
	case <-rewound:
	case <-time.After(20 * time.Second):
		t.Fatal("accepted terminal Undo did not call OnRewind")
	}
	select {
	case got := <-streamTerminal:
		if got == protocol.TMatchEnd {
			t.Fatal("match_end was emitted before the accepted terminal Undo rewound")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("focus stream did not receive rewind")
	}
	select {
	case <-ended:
		t.Fatal("OnMatchEnd fired for a match rewound out of game over")
	default:
	}
	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	state, over := m.state, m.e.G.Over
	m.mu.RUnlock()
	if state != protocol.MatchLive || over {
		t.Fatalf("terminal rewind left state=%s over=%v, want live/non-over", state, over)
	}
}

// TestUndoRacingFinishIsLinearizable pauses play immediately before finish's
// match-lock acquisition. Undo queues while finish is stopped at that barrier,
// so the boundary must consume it; if finish won the lock instead, Undo would
// have to return an error. It may never return success and disappear.
func TestUndoRacingFinishIsLinearizable(t *testing.T) {
	o := testOptions(t)
	atFinish := make(chan struct{})
	releaseFinish := make(chan struct{})
	rewound := make(chan struct{})
	ended := make(chan struct{}, 1)
	var enterOnce, releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFinish) }) })
	o.beforeFinish = func() {
		enterOnce.Do(func() { close(atFinish) })
		<-releaseFinish
	}
	o.OnRewind = func(_ TableID, _ int, _ int, _ uint64) error {
		close(rewound)
		return nil
	}
	o.OnMatchEnd = func(_ TableID, _ int, _ protocol.MatchInfo) error {
		ended <- struct{}{}
		return nil
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	addTerminalUndoTable(t, r, 0)
	driveHumanUntil(t, r, atFinish)

	// finish has observed G.Over but has not acquired m.mu. Undo can reserve
	// under that lock first, which fixes the previously lost-204 interleaving.
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo while finish was paused: %v", err)
	}
	releaseOnce.Do(func() { close(releaseFinish) })
	select {
	case <-rewound:
	case <-ended:
		t.Fatal("Undo returned success but finish emitted match_end")
	case <-time.After(20 * time.Second):
		t.Fatal("Undo returned success but neither rewind nor match_end was observed")
	}
}

// TestUndoRewindsToTheRequesterLastDecisionAndReplaysIdentically is the
// headline property: after an undo the requester is pending on the very
// decision their last intent answered, the log holds exactly the first n
// intents, and the truncated chain is the recorded log's own prefix — the
// same head a ReplayTo(n) reaches, byte for byte.
func TestUndoRewindsToTheRequesterLastDecisionAndReplaysIdentically(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	rewound := make(chan int, 1)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	r := undoTable(t, o, "t1")
	d1 := answerOnce(t, r, "t1", 0) // intent 0: the human's first action
	d2 := answerOnce(t, r, "t1", d1.Seq)
	waitIntents(t, r, "t1", 3) // bot intents land after the human's second action

	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	n, ok := lastIntentOf(m.e.L.Intents, 0)
	boundsBefore := append([]uint64(nil), m.bounds...)
	headBefore := m.e.L.Head()
	eventsBefore := len(m.e.L.Events)
	m.mu.RUnlock()
	if !ok {
		t.Fatal("the human seat has no logged intent")
	}
	// The recorded prefix the rewind must reproduce: its chain head is what
	// HeadAt over the FULL recorded log says it should be.
	m.mu.RLock()
	wantHead := m.e.L.HeadAt(int(boundsBefore[n]))
	m.mu.RUnlock()

	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := waitRewind(t, rewound); got != n {
		t.Fatalf("OnRewind intent %d, want %d", got, n)
	}
	m = liveMatch(t, r, "t1")
	m.mu.RLock()
	got := m.e.L
	head, intents, eventsCount := m.e.L.Head(), len(got.Intents), len(got.Events)
	m.mu.RUnlock()

	// The log is the prefix: n intents, bounds truncated with them, and the
	// chain head identical to the recorded log's own prefix head — the same
	// state hash ReplayTo(n) reaches.
	if intents != n {
		t.Fatalf("after undo the log holds %d intents, want %d (the requester's last intent index)", intents, n)
	}
	if head != wantHead {
		t.Fatalf("after undo chain head %s, want the recorded prefix head %s (ReplayTo(%d))", head, wantHead, n)
	}
	if eventsCount != int(boundsBefore[n]) {
		t.Fatalf("after undo the log holds %d events, want bounds[%d]=%d", eventsCount, n, boundsBefore[n])
	}
	// The requester is pending again on the decision their last intent
	// answered, and the live engine still verifies against its own log.
	d := waitPendingSeq(t, r, "t1", d2.Seq)
	if headBefore == head && eventsBefore == eventsCount {
		t.Fatal("nothing was rewound")
	}
	// The bots re-decide: answering the rewound decision advances the game
	// again (the discarded bot intents are re-derived from the rewound
	// state, never replayed from the old tail).
	if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
		t.Fatalf("SubmitIntent after undo: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		d3, err := r.Pending("t1", 1, 0)
		if err == nil && d3.Seq != d.Seq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("game did not advance after the rewound answer; last err=%v", err)
		}
		time.Sleep(time.Millisecond)
	}
	// And the whole truncated-plus-continued game still replays: replay the
	// truncated log alone through ReplayTo and compare the head at the
	// rewind point (the continuation needs new intents, which a replay of a
	// live match cannot have — the prefix check above already pins it).
	m.mu.RLock()
	continued := m.e.L.Clone()
	cfg := m.cfg
	m.mu.RUnlock()
	e, err := replay.ReplayTo(continued, cfg, n)
	if err != nil {
		t.Fatalf("ReplayTo over the truncated log diverged: %v", err)
	}
	if got := e.L.Head(); got != wantHead {
		t.Fatalf("ReplayTo(%d) head %s, recorded prefix head %s", n, got, wantHead)
	}
}

// TestUndoQueuePreservesEveryAcceptedRequest pins the structural queue behind
// Registry.Undo: the size-one channel is a wakeup, not storage, so two rapid
// accepted requests survive even while only one channel value can be buffered.
func TestUndoQueuePreservesEveryAcceptedRequest(t *testing.T) {
	t.Parallel()
	q := newUndoQueue()
	if err := q.request(0, 2); err != nil {
		t.Fatal(err)
	}
	if err := q.request(0, 2); err != nil {
		t.Fatal(err)
	}
	if err := q.request(0, 2); err == nil {
		t.Fatal("a third request overbooked two extant human intents")
	}

	first := <-q.signal
	select {
	case <-q.signal:
		t.Fatal("queue stored multiple wakeups instead of one pending count")
	default:
	}
	q.complete(first)
	second := <-q.signal
	q.complete(second)
	select {
	case <-q.signal:
		t.Fatal("queue remained armed after both requests completed")
	default:
	}
}

// TestRapidDoubleUndoPerformsTwoRewinds exercises the review break through
// Registry.Undo: two 204-equivalent accepted calls made back-to-back each
// produce an OnRewind and walk to a distinct earlier human decision.
func TestRapidDoubleUndoPerformsTwoRewinds(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	rewound := make(chan int, 2)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	r := undoTable(t, o, "t1")

	var seqs []uint64
	var last uint64
	for i := 0; i < 3; i++ {
		d := answerOnce(t, r, "t1", last)
		last = d.Seq
		seqs = append(seqs, d.Seq)
	}
	waitIntents(t, r, "t1", 5)

	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("first rapid Undo: %v", err)
	}
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("second rapid Undo: %v", err)
	}
	first, second := waitRewind(t, rewound), waitRewind(t, rewound)
	if second >= first {
		t.Fatalf("queued rewinds did not walk backward: intent boundaries %d then %d", first, second)
	}
	_ = waitPendingSeq(t, r, "t1", seqs[1])

	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	got := len(m.e.L.Intents)
	m.mu.RUnlock()
	if got != second {
		t.Fatalf("after two accepted requests log holds %d intents, second rewind reported %d", got, second)
	}
}

// TestRepeatedUndoWalksBackOneHumanIntentAtATime: every undo removes exactly
// one of the requester's own actions (bot intents between go with it), until
// the requester has none left and the seat is pending on the game's very
// first decision — full rollback by repetition.
func TestRepeatedUndoWalksBackOneHumanIntentAtATime(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	rewound := make(chan int, 3)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	r := undoTable(t, o, "t1")

	// Record the decision seqs of the human's first three actions in order;
	// each undo must land pending on the previous one.
	var seqs []uint64
	var last uint64
	for i := 0; i < 3; i++ {
		d := answerOnce(t, r, "t1", last)
		last = d.Seq
		seqs = append(seqs, d.Seq)
	}
	waitIntents(t, r, "t1", 5) // bots play between/after the human's actions

	for i := len(seqs) - 1; i >= 0; i-- {
		if err := r.Undo("t1", 1, 0); err != nil {
			t.Fatalf("undo %d: %v", i, err)
		}
		toIntent := waitRewind(t, rewound)
		_ = waitPendingSeq(t, r, "t1", seqs[i])
		m := liveMatch(t, r, "t1")
		m.mu.RLock()
		ins := len(m.e.L.Intents)
		m.mu.RUnlock()
		if ins != toIntent {
			t.Fatalf("after undo %d the log holds %d intents, hook reported %d", i, ins, toIntent)
		}
		if i > 0 && ins == 0 {
			t.Fatalf("after undo %d the log holds no intents; earlier human intents should remain", i)
		}
		// Do not answer the rewound decision: another press removes the next
		// earlier human intent. Re-submitting here would restore the action we
		// just removed and could only undo that same action forever.
	}
	// Rollback complete: the requester has no intents left and one more undo
	// is refused — the seat sits at the game's first decision.
	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	ins := append([]decision.Intent(nil), m.e.L.Intents...)
	m.mu.RUnlock()
	for _, in := range ins {
		if in.Player == 0 {
			t.Fatalf("full rollback left a human intent in the log: %+v", in)
		}
	}
	if err := r.Undo("t1", 1, 0); err == nil {
		t.Fatal("an undo with no human intent left to undo was accepted")
	}
}

// TestUndoTruncatesDiskAndAppendsFromTheNewEnd proves the built-in file
// embedder does not retain the discarded tail or leave a sparse hole when
// ordinary one-intent bursts resume after a rewind.
func TestUndoTruncatesDiskAndAppendsFromTheNewEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	o := diskOptions(t, dir)
	rewound := make(chan int, 1)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	r := undoTable(t, o, "t1")
	d1 := answerOnce(t, r, "t1", 0)
	d2 := answerOnce(t, r, "t1", d1.Seq)
	waitIntents(t, r, "t1", 3)

	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	n, ok := lastIntentOf(m.e.L.Intents, 0)
	wantEvents := int(m.bounds[n])
	m.mu.RUnlock()
	if !ok {
		t.Fatal("human intent missing")
	}
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatal(err)
	}
	if got := waitRewind(t, rewound); got != n {
		t.Fatalf("rewound to %d intents, want %d", got, n)
	}
	persisted, err := readLog(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Intents) != n || len(persisted.Events) != wantEvents {
		t.Fatalf("persisted rewind has %d intents/%d events, want %d/%d", len(persisted.Intents), len(persisted.Events), n, wantEvents)
	}

	d := waitPendingSeq(t, r, "t1", d2.Seq)
	if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if next, err := r.Pending("t1", 1, 0); err == nil && next.Seq != d.Seq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("post-rewind answer did not reach the next human decision")
		}
		time.Sleep(time.Millisecond)
	}
	persisted, err = readLog(dir, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Intents) <= n || len(persisted.Events) <= wantEvents {
		t.Fatalf("post-rewind append did not extend truncated files: %d intents/%d events", len(persisted.Intents), len(persisted.Events))
	}
}

// TestUndoRefusedOnTwoHumanTables: the consent fence. A table whose Humans
// list has a second seat refuses the undo no matter who asks.
func TestUndoRefusedOnTwoHumanTables(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "t1", Name: "two-humans", Seats: 4, Decks: []string{"a", "b", "c", "d"},
		Seed: 5, Spectator: view.Omniscient, Humans: []int{0, 1}}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}
	waitPending(t, r, "t1", 1, 0)
	for _, seat := range []state.PlayerID{0, 1} {
		if err := r.Undo("t1", 1, seat); err == nil {
			t.Fatalf("undo by seat %d was accepted on a two-human table", seat)
		} else if !stringContains(err.Error(), "human seats") {
			t.Fatalf("undo rejection by seat %d does not name the consent reason: %v", seat, err)
		}
	}
	// And a bot-only table refuses for its own reason.
	o2 := testOptions(t)
	r2, err := New(o2)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if err := r2.AddTable(fourSeatTable("t2", false)); err != nil {
		t.Fatal(err)
	}
	if err := r2.Start("t2"); err != nil {
		t.Fatal(err)
	}
	if err := r2.Undo("t2", 1, 0); err == nil {
		t.Fatal("undo was accepted on a bots-only table")
	}
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestUndoHookFiresOnceAndOnBurstStaysOneIntentPerBurst is the v1 MAJOR as a
// regression test. OnRewind must fire exactly once per undo with the intent
// index the match was rewound to, and after the rewind every OnBurst call
// must still carry exactly ONE intent's burst — the v1 fork pushed the whole
// inherited prefix through one OnBurst call (many intents, one call), which
// broke every embedder's persistence. The structural detector: every
// non-genesis burst contains at most one DecisionAsk (its own closing one),
// because a burst is exactly one Submit's run to the next ask.
func TestUndoHookFiresOnceAndOnBurstStaysOneIntentPerBurst(t *testing.T) {
	t.Parallel()
	var (
		obsMu    sync.Mutex
		bursts   []burstObs
		rewinds  []int
		rewHeads []uint64
	)
	rewound := make(chan int, 1)
	o := testOptions(t)
	o.OnBurst = func(_ TableID, k int, evs []events.Event, in *decision.Intent) error {
		if k != 1 {
			t.Errorf("OnBurst for match %d, want 1", k)
		}
		var copied *decision.Intent
		if in != nil {
			v := *in
			copied = &v
		}
		obsMu.Lock()
		bursts = append(bursts, burstObs{evs: append([]events.Event(nil), evs...), in: copied})
		obsMu.Unlock()
		return nil
	}
	o.OnRewind = func(_ TableID, k int, toIntent int, headSeq uint64) error {
		if k != 1 {
			t.Errorf("OnRewind for match %d, want 1", k)
		}
		obsMu.Lock()
		rewinds = append(rewinds, toIntent)
		rewHeads = append(rewHeads, headSeq)
		obsMu.Unlock()
		rewound <- toIntent
		return nil
	}
	r := undoTable(t, o, "t1")
	d1 := answerOnce(t, r, "t1", 0)
	d2 := answerOnce(t, r, "t1", d1.Seq)
	waitIntents(t, r, "t1", 3)

	// The expected rewind point is stable: bots may append intents after the
	// human's last one, but the index of the human's LAST intent cannot move.
	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	n, ok := lastIntentOf(m.e.L.Intents, 0)
	wantHeadSeq := m.bounds[n] - 1
	m.mu.RUnlock()
	if !ok {
		t.Fatal("no human intent to compute the rewind point from")
	}

	obsMu.Lock()
	burstsBefore := len(bursts)
	obsMu.Unlock()
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := waitRewind(t, rewound); got != n {
		t.Fatalf("OnRewind intent %d, want %d", got, n)
	}
	// Keep playing past the rewind, proving later observations remain normal
	// one-intent bursts rather than replaying the inherited prefix.
	d := waitPendingSeq(t, r, "t1", d2.Seq)
	if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
		t.Fatalf("SubmitIntent after undo: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		d3, err := r.Pending("t1", 1, 0)
		if err == nil && d3.Seq != d.Seq {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("game did not advance after rewound answer: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	obsMu.Lock()
	defer obsMu.Unlock()
	if len(rewinds) != 1 {
		t.Fatalf("OnRewind fired %d times, want exactly 1", len(rewinds))
	}
	if rewinds[0] != n {
		t.Fatalf("OnRewind toIntent %d, want %d (the requester's last intent index)", rewinds[0], n)
	}
	if rewHeads[0] != wantHeadSeq {
		t.Fatalf("OnRewind headSeq %d, want %d", rewHeads[0], wantHeadSeq)
	}
	// Every non-genesis burst carries exactly one intent's run: at most one
	// DecisionAsk (the v1 fork's whole-prefix burst carried one per intent).
	for i, b := range bursts {
		if b.in == nil {
			if i != 0 {
				t.Fatalf("burst %d has no intent; only the genesis burst may", i)
			}
			continue
		}
		asks := 0
		for _, ev := range b.evs {
			if ev.Kind == events.DecisionAsk {
				asks++
			}
		}
		if asks > 1 {
			t.Fatalf("burst %d carries %d DecisionAsks — more than one intent's burst (the v1 MAJOR shape)", i, asks)
		}
	}
	if len(bursts) <= burstsBefore {
		t.Fatalf("no OnBurst bursts after the undo (got %d, had %d)", len(bursts), burstsBefore)
	}
}

// TestUndoStreamReceivesRewindThenConsistentFrames: a connected focus
// subscriber sees the rewind frame (a full snapshot at the new head) and
// then only frames consistent with it — the rewind's decision frame, and
// event frames that never exceed the new head.
func TestUndoStreamReceivesRewindThenConsistentFrames(t *testing.T) {
	t.Parallel()
	r := undoTable(t, testOptions(t), "t1")
	s := r.OpenSession()
	defer r.CloseSession(s.ID)
	frames := make(chan protocol.Frame, 1024)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for f := range s.Out() {
			frames <- f
		}
	}()
	if err := r.Subscribe(s, "t1", protocol.ModeFocus); err != nil {
		t.Fatal(err)
	}
	d1 := answerOnce(t, r, "t1", 0)
	answerOnce(t, r, "t1", d1.Seq)
	waitIntents(t, r, "t1", 3)

	// The pre-undo head, read from the live match under its lock. The
	// subscriber's frame pump is asynchronous, so a non-blocking drain of
	// the stream cannot serve as the measure: under load the drain samples
	// before the last answer's event frames reach the channel, and the
	// assertion then compared the rewind head against a stale maximum
	// (sporadic "rewound head 36 is not below the pre-undo head 36" in
	// full-module gate runs). Rewind snapshots carry fanout.go's head()
	// — len(events)-1 — so this is the same measure, race-free, and the
	// comparison is strictly stronger than the drained maximum ever was.
	mm := liveMatch(t, r, "t1")
	mm.mu.RLock()
	preUndoHead := head(mm)
	mm.mu.RUnlock()
	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var rewindSeen, decisionAfter bool
	var newHead uint64
	for time.Now().Before(deadline) {
		select {
		case f := <-frames:
			switch f.T {
			case protocol.TRewind:
				var snap protocol.Snapshot
				if err := f.Decode(&snap); err != nil {
					t.Fatalf("rewind body: %v", err)
				}
				newHead = snap.Head
				if newHead >= preUndoHead {
					t.Fatalf("rewound head %d is not below the pre-undo head %d", newHead, preUndoHead)
				}
				if len(snap.View.Players) == 0 {
					t.Fatal("rewind snapshot carries no board")
				}
				rewindSeen = true
			case protocol.TDecision:
				if rewindSeen {
					decisionAfter = true
				}
			case protocol.TEvent:
				if rewindSeen && f.Seq > newHead {
					t.Fatalf("event frame at seq %d after a rewind to head %d", f.Seq, newHead)
				}
			}
		default:
			if rewindSeen && decisionAfter {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatalf("stream never saw rewind=%v decision-after=%v", rewindSeen, decisionAfter)
}
