package host

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/protocol"
)

// TestPendingBeforeSeatsInstalledIsNothingPending: a match is visible as
// live (newMatch) a moment before play() installs its seats. A Pending poll
// in that window must read as "no decision pending yet" -- the answer a
// poller retries -- not as the player being out of range of a 0-seat match,
// which flaked TestUndoHookFiresOnceAndOnBurstStaysOneIntentPerBurst under
// load.
func TestPendingBeforeSeatsInstalledIsNothingPending(t *testing.T) {
	r, err := New(testOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	r.mu.Lock()
	r.tables["seatless"] = &table{k: 1, cur: &match{k: 1, state: protocol.MatchLive}}
	r.mu.Unlock()
	// Cleanups run last-in first-out: the bare table leaves before Close,
	// which would otherwise try to stop a table that was never started.
	t.Cleanup(func() { r.mu.Lock(); delete(r.tables, "seatless"); r.mu.Unlock() })
	_, err = r.Pending("seatless", 1, 0)
	if err == nil || !strings.Contains(err.Error(), "no decision pending") {
		t.Fatalf("Pending before seats are installed = %v, want a no-decision-pending error", err)
	}
}
