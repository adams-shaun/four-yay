package host

// BP-09 (spec 2026-09-28-hosted-bot-packages §5.2 Undo): after an undo, every
// live EnvSeat feed equals searchseat.RebuildFeed at the new head, and play
// continues. The pre-undo feed observed every decision of the discarded tail,
// so only a rebuild from the truncated log restores the live-equals-rebuild
// invariant BP-07 pinned (TestHostFeedEqualsRebuildFeed).

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/internal/searchprobe"
	"github.com/adams-shaun/gorge/internal/searchseat"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/view"
)

// snapshotHistory copies a live feed's History for comparison after the lock
// is dropped. Feed.History returns the struct by value, which freezes the
// Frames slice header (appends land past the snapshot's len and cannot touch
// it, and a non-scratch capture's Board is a fresh json.Marshal copy), but
// the Answers map is STILL SHARED with the live feed: RecordAnswer keeps
// inserting entries into it after the lock is released, so DeepEqual over
// that map races the play loop (measured: -count=3 failed "answers 7/5" with
// an extra entry on the pending-decision frame, identical frame decision
// sequences — a shared-map growth, not a log divergence). Rebuild the map
// into a fresh one; the []Action values may be shared because RecordAnswer
// replaces whole slices, never mutates one in place.
func snapshotHistory(f *searchseat.Feed) searchprobe.History {
	h := f.History()
	answers := make(map[int][]searchprobe.Action, len(h.Answers))
	for k, v := range h.Answers {
		answers[k] = v
	}
	h.Answers = answers
	return h
}

func TestUndoRebuildsEnvSeatFeeds(t *testing.T) {
	t.Parallel()
	o := testOptions(t)
	rewound := make(chan int, 1)
	o.OnRewind = func(_ TableID, _ int, n int, _ uint64) error { rewound <- n; return nil }
	o.Seats = func(names []string, gotSeed uint64) []seat.Seat {
		// Slot 0 is replaced by the HumanSeat through TableConfig.Humans
		// below (the undo requester); slot 1 stays the spy EnvSeat, the only
		// seat the host builds a feed for.
		return []seat.Seat{
			seat.NewBot(gotSeed ^ 1),
			&spyEnvSeat{Bot: seat.NewBot(gotSeed ^ 2)},
		}
	}
	r, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cfg := TableConfig{ID: "t1", Name: "undofeeds", Seats: 2, Decks: []string{"a", "b"},
		Seed: 42, Pace: 0, Spectator: view.Omniscient, Humans: []int{0}}
	if err := r.AddTable(cfg); err != nil {
		t.Fatal(err)
	}
	if err := r.Start("t1"); err != nil {
		t.Fatal(err)
	}

	answerOnce(t, r, "t1") // the human's first intent
	d2 := answerOnce(t, r, "t1")
	waitIntents(t, r, "t1", 4) // bot intents land after the human's second action

	m := liveMatch(t, r, "t1")
	m.mu.RLock()
	if m.feeds == nil || m.feeds.bySlot[1] == nil {
		m.mu.RUnlock()
		t.Fatal("the spy EnvSeat has no feed: Create is not wired")
	}
	if m.feeds.bySlot[0] != nil {
		m.mu.RUnlock()
		t.Fatal("the human slot has a feed; only EnvSeats must get one")
	}
	feedBefore := m.feeds.bySlot[1]
	if !feedBefore.Live() {
		stop := feedBefore.StopReason()
		m.mu.RUnlock()
		t.Fatalf("the live feed stopped before the undo: %s", stop)
	}
	framesBefore := feedBefore.Frames()
	intentsBefore := len(m.e.L.Intents)
	m.mu.RUnlock()

	// Preconditions the real assertion depends on: the game reached a rewind
	// boundary that left a decision tail behind, and the feed had observed
	// past it. A feed that is never rebuilt keeps exactly those tail frames,
	// so a skipped rebuild cannot pass the frame-count assertion below.
	if intentsBefore < 4 {
		t.Fatalf("match logged only %d intents; no tail to discard", intentsBefore)
	}
	if framesBefore < 3 {
		t.Fatalf("the feed captured only %d frames; Observe is not wired", framesBefore)
	}

	if err := r.Undo("t1", 1, 0); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := waitRewind(t, rewound); got < 1 || got >= intentsBefore {
		t.Fatalf("OnRewind intent %d is not inside the recorded prefix", got)
	}

	// The feeds are rebuilt inside rewindToLastIntent, under the same lock
	// and before its projectNext runs, so by the time the hook fired the
	// rebuild is final for this boundary.
	m = liveMatch(t, r, "t1")
	m.mu.RLock()
	feedAfter := m.feeds.bySlot[1]
	lcfg := m.cfg
	lclone := m.e.L.Clone()
	intents := len(m.e.L.Intents)
	// Snapshot the rebuilt feed under the same lock as the log clone: the
	// play loop is parked on the rewound decision here, but reading a live
	// feed outside m.mu is the race this test must not have (the final block
	// below was caught racing it — the 2026-09-29 module gate failure).
	aHist := snapshotHistory(feedAfter)
	aColl := feedAfter.Collector().Clone()
	m.mu.RUnlock()
	if feedAfter == nil {
		t.Fatal("the feed is gone after the undo")
	}
	if feedAfter == feedBefore {
		t.Fatal("the pre-undo feed object is still installed: the rebuild never ran")
	}
	if !feedAfter.Live() {
		t.Fatalf("the rebuilt feed is stopped: %s", feedAfter.StopReason())
	}
	if feedAfter.Frames() >= framesBefore {
		t.Fatalf("after the undo the feed holds %d frames, the pre-undo feed %d: the discarded tail is still in the history",
			feedAfter.Frames(), framesBefore)
	}

	// The rebuilt feed equals RebuildFeed at the new head — the claim that
	// makes the feed derived state the undo may regenerate.
	framesAtRewind := feedAfter.Frames()
	rebuilt, err := searchseat.RebuildFeed(lcfg, lclone, intents, 1)
	if err != nil {
		t.Fatalf("RebuildFeed: %v", err)
	}
	if !reflect.DeepEqual(aHist, rebuilt.History()) {
		ah, rh := aHist, rebuilt.History()
		t.Fatalf("post-undo feed != RebuildFeed(%d): frames %d/%d answers %d/%d",
			intents, len(ah.Frames), len(rh.Frames), len(ah.Answers), len(rh.Answers))
	}
	if !reflect.DeepEqual(aColl, rebuilt.Collector().Clone()) {
		t.Fatal("post-undo feed collector state != RebuildFeed's")
	}

	// Play continues: the rewound pending decision is answerable, the game
	// advances past it, and the REBUILT feed keeps tracking — it must equal
	// RebuildFeed at the new head again (a host that kept observing the
	// stale pre-undo object would fail here, as would a rebuild that was
	// never installed).
	d := waitPendingSeq(t, r, "t1", d2.Seq)
	if err := r.SubmitIntent("t1", 1, 0, legalIntent(d)); err != nil {
		t.Fatalf("SubmitIntent after undo: %v", err)
	}
	waitIntents(t, r, "t1", intents+3)

	m = liveMatch(t, r, "t1")
	m.mu.RLock()
	finalFeed := m.feeds.bySlot[1]
	fcfg := m.cfg
	fclone := m.e.L.Clone()
	fintents := len(m.e.L.Intents)
	// The history and the collector are snapshotted under the SAME lock that
	// cloned the log. Reading finalFeed.History() after the RUnlock — as the
	// first cut of this test did — races the live loop: waitIntents returns
	// the moment the intent count is reached, mid-burst, and the bots keep
	// submitting while RebuildFeed walks the stale clone, so the comparison
	// measured the live feed one intent past the prefix it was compared to
	// (frames 10/9, answers 7/6 — exactly one extra trailing frame+answer).
	// Under m.mu the real submit path is consistent by construction (Submit,
	// feeds.record and projectNext's observe are one locked section), so the
	// snapshot is a valid prefix boundary at ANY quiescent-or-not instant;
	// snapshotHistory freezes the answers map too, which a bare History()
	// value copy would still share with the live feed.
	fHist := snapshotHistory(finalFeed)
	fColl := finalFeed.Collector().Clone()
	m.mu.RUnlock()
	if finalFeed == nil {
		t.Fatal("the feed is gone after continued play")
	}
	if finalFeed == feedBefore {
		t.Fatal("the stale pre-undo feed came back")
	}
	again, err := searchseat.RebuildFeed(fcfg, fclone, fintents, 1)
	if err != nil {
		t.Fatalf("RebuildFeed after continued play: %v", err)
	}
	if !reflect.DeepEqual(fHist, again.History()) {
		fh, rh := fHist, again.History()
		t.Fatalf("continued feed != RebuildFeed(%d): frames %d/%d answers %d/%d",
			fintents, len(fh.Frames), len(rh.Frames), len(fh.Answers), len(rh.Answers))
	}
	if !reflect.DeepEqual(fColl, again.Collector().Clone()) {
		t.Fatal("continued feed collector state != RebuildFeed's")
	}
	if finalFeed.Frames() <= framesAtRewind {
		t.Fatalf("the rebuilt feed recorded no post-undo frames (%d <= %d): the host is not observing the rebuilt feed",
			finalFeed.Frames(), framesAtRewind)
	}
}
