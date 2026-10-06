package events

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// appendRun appends n distinct events to l, the same sequence every call, so
// two logs built from it are byte-identical and their heads comparable.
func appendRun(l *Log, n int) {
	for i := 0; i < n; i++ {
		l.Append(Event{Kind: Priority, Player: state.PlayerID(i % 2), Amount: int32(i)})
	}
}

// TestRecycledSpareKeepsItsCapacity pins the mechanism that removes the live
// game's regrowth copies: NewLogInto keeps a recycled event array's full
// capacity, so a log built from a long finished game's spent array runs back
// up to that length without a single growEvents reallocation. Capacity is not
// observable (the events, Seq and chain are identical from any starting
// capacity), which is what makes retaining it a pure allocation win.
//
// Precondition: the first run must have outgrown the default preallocation,
// or there is no capacity to retain and the test proves nothing.
func TestRecycledSpareKeepsItsCapacity(t *testing.T) {
	const n = 6000
	first := NewLog(1)
	appendRun(first, n)
	firstCap := cap(first.Events)
	if firstCap <= expectedEventsPerGame {
		t.Fatalf("precondition: first log cap = %d, want > %d (it must have grown past the preallocation)", firstCap, expectedEventsPerGame)
	}
	firstHead := first.Head()
	// Release it exactly as a batch runner does: the whole capacity, lazily
	// (rules.Engine.Release).
	spare := first.Events[:cap(first.Events)]

	second := NewLogInto(1, spare)
	if got := cap(second.Events); got != firstCap {
		t.Fatalf("recycled log cap = %d, want the spare's %d: retaining it is the point", got, firstCap)
	}
	// One append first, so Events[0] is addressable, then every later append
	// must reuse the same backing array. A re-capped (4096) spare would
	// reallocate here, which is the defect this pins.
	second.Append(Event{Kind: Priority, Player: 0, Amount: 0})
	base := &second.Events[0]
	for i := 1; i < n; i++ {
		second.Append(Event{Kind: Priority, Player: state.PlayerID(i % 2), Amount: int32(i)})
		if &second.Events[0] != base {
			t.Fatalf("append %d reallocated: the recycled capacity was thrown away", i)
		}
	}
	if len(second.Events) != n {
		t.Fatalf("second log has %d events, want %d", len(second.Events), n)
	}
	if second.Head() != firstHead {
		t.Fatalf("recycled log head = %s, fresh head = %s: capacity must not change the chain", second.Head(), firstHead)
	}
}

// TestNewLogIntoHintPreallocates pins option 2's plumbing: a positive
// ExpectedEvents hint is honoured as a starting capacity, a hint at or below
// the default preallocation is ignored, and the hint changes nothing a reader
// sees -- a hinted log and a default log over the same events have the same
// length, Seq and head.
func TestNewLogIntoHintPreallocates(t *testing.T) {
	const hint = 12000
	hinted := NewLogIntoHint(7, nil, hint)
	if cap(hinted.Events) < hint {
		t.Fatalf("hinted cap = %d, want >= %d", cap(hinted.Events), hint)
	}
	// Appending the whole hint must never reallocate.
	base := cap(hinted.Events)
	appendRun(hinted, hint)
	if cap(hinted.Events) != base {
		t.Fatalf("appends within the hint reallocated: cap %d -> %d", base, cap(hinted.Events))
	}

	below := NewLogIntoHint(7, nil, 10)
	if cap(below.Events) != expectedEventsPerGame {
		t.Fatalf("hint below the default gave cap %d, want %d", cap(below.Events), expectedEventsPerGame)
	}

	// Same events, different starting capacities: identical log.
	plain := NewLogInto(7, nil)
	appendRun(plain, 3000)
	shrink := NewLogIntoHint(7, nil, 10)
	appendRun(shrink, 3000)
	if plain.Head() != shrink.Head() || len(plain.Events) != len(shrink.Events) {
		t.Fatalf("capacity leaked into the log: head %s/%s len %d/%d",
			plain.Head(), shrink.Head(), len(plain.Events), len(shrink.Events))
	}
}

// TestHintedSpareRetainsLargerCapacity pins the two mechanisms together: when
// both a hint and a recycled array are supplied, the larger of the two wins,
// so a hint never discards a spare that already grew further.
func TestHintedSpareRetainsLargerCapacity(t *testing.T) {
	first := NewLog(3)
	appendRun(first, 6000)
	bigCap := cap(first.Events)
	if bigCap <= expectedEventsPerGame {
		t.Fatalf("precondition: spare cap = %d, want > %d", bigCap, expectedEventsPerGame)
	}
	spare := first.Events[:cap(first.Events)]

	// Hint larger than the spare: allocate the hint, ignore the small spare.
	l := NewLogIntoHint(3, spare, bigCap*2)
	if cap(l.Events) < bigCap*2 {
		t.Fatalf("hint above the spare gave cap %d, want >= %d", cap(l.Events), bigCap*2)
	}
	// Spare larger than the hint: keep the spare's full capacity.
	l2 := NewLogIntoHint(3, spare, expectedEventsPerGame)
	if cap(l2.Events) != bigCap {
		t.Fatalf("hint below the spare gave cap %d, want the spare's %d", cap(l2.Events), bigCap)
	}
}
