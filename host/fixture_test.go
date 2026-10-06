package host

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/internal/testbudget"
)

// TestMain closes the shared finished match below once, after every test
// that borrowed it has finished.
//
// Not here on purpose: debug.SetGCPercent. Tried at 400 (the suite spends
// ~50% of CPU in GC): a single match peaks near 1GB live, so with every
// test parallel the run thrashed instead — 99s, 82s of it system time.
// Parallelism is bounded by matchSlots instead.
func TestMain(m *testing.M) {
	testbudget.Main(m, func() {
		sharedFixture.mu.Lock()
		if sharedFixture.r != nil {
			sharedFixture.r.Close()
		}
		sharedFixture.mu.Unlock()
		modalFixture.mu.Lock()
		if modalFixture.r != nil {
			modalFixture.r.Close()
		}
		modalFixture.mu.Unlock()
	})
}

// matchSlots bounds how many tests -- i.e. matches, each ~1GB peak and one
// core -- run at once across the parallel tests. testOptions takes a slot
// for the test's lifetime. 8 keeps a 32-core box busy without paging; the
// un-gated version (33 matches at once) was slower than sequential.
var matchSlots = newMatchLimiter(matchSlotCount())

// matchSlotCount is 8, or 4 under -race (raceEnabled, fixture_race_test.go):
// the race detector multiplies each match's memory several times over, and
// the only -race run is the pre-push gate on main (FL-56), where not paging
// matters more than wall time.
func matchSlotCount() int {
	if raceEnabled {
		return 4
	}
	return 8
}

// matchLimiter is a counting semaphore whose unit is a TOP-LEVEL TEST, not a
// registry. A test (with all of its subtests) holds at most one slot no
// matter how many times it calls testOptions: the first call takes the
// slot, later calls from the same test tree only add a reference, and the
// slot is returned when the last reference's test finishes.
//
// It used to be a bare channel send per testOptions call. A test that
// builds two registries (TestUndoRefusedOnTwoHumanTables: a two-human
// table parked on a human decision, then a second registry) then held one
// slot while waiting for another -- hold-and-wait -- and once the other
// parallel tests held the rest, each parked on a human decision of its own
// and waiting for nothing, the package hung until the 10-minute test
// timeout. TestHostedPoliciesReplayDeterministically (eight sequential
// testOptions calls on the parent t) could not even run alone under -race,
// where there are only four slots. Charging per test removes the second
// acquire, so no test ever waits for a slot while holding one.
type matchLimiter struct {
	slots chan struct{}
	mu    sync.Mutex
	holds map[string]*slotHold
}

type slotHold struct {
	mu sync.Mutex // serialises one test tree's acquire/release
	n  int        // live references; the slot is held while n > 0
}

func newMatchLimiter(n int) *matchLimiter {
	return &matchLimiter{slots: make(chan struct{}, n), holds: map[string]*slotHold{}}
}

// take charges t's top-level test one slot (blocking only if that test
// holds none yet) and releases the reference when t finishes.
func (l *matchLimiter) take(t *testing.T) {
	t.Helper()
	root, _, _ := strings.Cut(t.Name(), "/")
	l.mu.Lock()
	h := l.holds[root]
	if h == nil {
		h = &slotHold{}
		l.holds[root] = h
	}
	l.mu.Unlock()
	// Blocking under h.mu only ever blocks this test's own tree, which by
	// construction holds no slot yet (n == 0).
	h.mu.Lock()
	if h.n == 0 {
		l.slots <- struct{}{}
	}
	h.n++
	h.mu.Unlock()
	t.Cleanup(func() {
		h.mu.Lock()
		h.n--
		if h.n == 0 {
			<-l.slots
		}
		h.mu.Unlock()
	})
}

func takeMatchSlot(t *testing.T) {
	t.Helper()
	matchSlots.take(t)
}

var sharedFixture struct {
	once sync.Once
	mu   sync.Mutex
	r    *Registry
	m    *match
	err  error
}

// finishedTable returns a Registry whose table "t1" has played exactly one
// match — fourSeatTable("t1", false), seed 99 — to completion, and that
// finished match. The match is played ONCE per test binary and shared:
// seed 99 makes it a pure function of the configuration
// (TestTheSameConfigurationPlaysTheSameMatch), and every caller only reads
// (Registry.ViewAt/Events, the finished engine's log, bounds, snapshots).
//
// Callers must not Start, AddTable, Close, or otherwise mutate the
// registry or the match; a test that needs a table of its own (a live
// one, a Public one, a second table) builds it with New(testOptions(t)) as
// before. The registry is closed by TestMain, not by any one test.
func finishedTable(t *testing.T) (*Registry, *match) {
	t.Helper()
	sharedFixture.once.Do(func() {
		r, err := New(testOptions(t))
		if err != nil {
			sharedFixture.err = err
			return
		}
		if err := r.AddTable(fourSeatTable("t1", false)); err != nil {
			r.Close()
			sharedFixture.err = err
			return
		}
		if err := r.Start("t1"); err != nil {
			r.Close()
			sharedFixture.err = err
			return
		}
		r.Wait("t1")
		r.mu.RLock()
		tb := r.tables["t1"]
		r.mu.RUnlock()
		sharedFixture.mu.Lock()
		sharedFixture.r, sharedFixture.m = r, tb.history[0]
		sharedFixture.mu.Unlock()
	})
	if sharedFixture.err != nil {
		t.Fatalf("shared finished table: %v", sharedFixture.err)
	}
	return sharedFixture.r, sharedFixture.m
}
