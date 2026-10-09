package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestQuietProofScansServedFromWalkCache is the Q4 mechanism test: the proof
// must run inside a derived-memo scope so its board scans take the walk's
// board-only caches (rules/walkcache.go) instead of rescanning the whole
// board every window. quietProofScans increments exactly when quietBlocker
// runs with no live scope (walkKeyNow reports !ok), i.e. when it is about to
// pay uncached board scans; it must stay 0 across posed priority windows with
// the serve on. The serve delta asserts the proof was actually consulted, so
// a fixture that never reaches the proof fails loudly instead of passing.
func TestQuietProofScansServedFromWalkCache(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	quietServeTestOn(t, func(t *testing.T) {
		e := quietBase(t, reg)
		if !e.seatQuiet(0) {
			t.Fatalf("precondition: the seat is not proved quiet (blocker %s)", quietBlockerNames[e.quietBlocker(0)])
		}
		const windows = 4
		scans0 := quietProofScans.Load()
		served0 := quietServedCount()
		for i := 0; i < windows; i++ {
			e.priorityOptions(0, nil)
		}
		if n := quietServedCount() - served0; n != windows {
			t.Fatalf("precondition: the serve ran %d of %d windows, want %d: the proof is not being consulted", n, windows, windows)
		}
		if n := quietProofScans.Load() - scans0; n != 0 {
			t.Fatalf("the proof paid %d uncached board scans across %d served windows, want 0: quietBlocker is not inside a derived-memo scope", n, windows)
		}
	})
}
