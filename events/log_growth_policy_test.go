package events

import (
	"slices"
	"testing"
)

// TestGrowEventsFreshLogDoublesThroughLiveRange pins the growth shape of an
// unpresized log. A fresh NewLog starts at expectedEventsPerGame (4096) and a
// live game runs to ~10300 events; every reallocation on the way is a copy of
// the whole history, so the climb must double (4096 -> 8192 -> 16384, copying
// 4096 + 8192 events) rather than taper at 1.25x from 4096 (5120, 6400, 8000,
// 10000, 12500: five copies). Capacity is not observable in events, Seq or the
// chain, so the transitions themselves are what is asserted.
//
// Precondition: the log starts at the default preallocation and the run
// outgrows 8192, or the ladder is trivial.
func TestGrowEventsFreshLogDoublesThroughLiveRange(t *testing.T) {
	const n = 10300
	l := NewLog(1)
	if got := cap(l.Events); got != expectedEventsPerGame {
		t.Fatalf("precondition: fresh log cap = %d, want %d", got, expectedEventsPerGame)
	}
	if n <= 2*expectedEventsPerGame {
		t.Fatalf("precondition: run of %d must exceed %d for a non-trivial ladder", n, 2*expectedEventsPerGame)
	}
	var ladder []int
	last := cap(l.Events)
	for i := 0; i < n; i++ {
		appendRun(l, 1)
		if c := cap(l.Events); c != last {
			ladder = append(ladder, c)
			last = c
		}
	}
	if want := []int{8192, 16384}; !slices.Equal(ladder, want) {
		t.Fatalf("capacity ladder = %v, want %v (doubling through the live-game range)", ladder, want)
	}
}

// TestGrowEventsLargeStartLogStillTapers guards the other half of the policy:
// a deliberately large-start array (here a 20000-slot hint) that overflows by
// one grows 1.25x, not 2x, so a presized log that runs slightly over does not
// allocate an array half of which is never written. It passes both with and
// without the doubling fix, by design: it exists to stop the taper being
// removed outright, not to detect the fresh-log fix.
//
// Precondition: the start is at or above growTaperAt and the log is full, so
// the next append is the first grow.
func TestGrowEventsLargeStartLogStillTapers(t *testing.T) {
	const start = 20000
	if start < growTaperAt {
		t.Fatalf("precondition: start %d must be >= growTaperAt %d", start, growTaperAt)
	}
	l := NewLogIntoHint(1, nil, start)
	if got := cap(l.Events); got != start {
		t.Fatalf("precondition: hinted log cap = %d, want %d", got, start)
	}
	appendRun(l, start)
	if cap(l.Events) != start {
		t.Fatalf("precondition: log grew early, cap = %d", cap(l.Events))
	}
	appendRun(l, 1)
	if got, want := cap(l.Events), start+start/growTaperDiv; got != want {
		t.Fatalf("cap after first overflow = %d, want %d (1.25x taper, not %d)", got, want, 2*start)
	}
}
