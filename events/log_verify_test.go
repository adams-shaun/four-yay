package events

import (
	"errors"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// verifyLog builds a log of n distinct events past a checkpoint clone taken
// after the first k: what rules' resolution kernel holds when it rewinds to
// its checkpoint (the clone) and re-executes the recorded tail.
func verifyLog(k, n int) (l, s0 *Log) {
	l = NewLog(11)
	for i := 0; i < k; i++ {
		l.Append(Event{Kind: Draw, Player: state.PlayerID(i % 2), Amount: int32(i)})
	}
	s0 = l.Clone()
	for i := k; i < n; i++ {
		l.Append(Event{Kind: Damage, Obj: state.ObjID(i), Amount: int32(i), IDs: []state.ObjID{1, state.ObjID(i)}})
	}
	return l, s0
}

// TestRewindToReproducesTheRecordedTail pins the verify window: after
// RewindTo, re-appending exactly the recorded events stores nothing new,
// leaves every stored event and the chain head as they were, and closes
// clean; appends past the window extend the log normally.
func TestRewindToReproducesTheRecordedTail(t *testing.T) {
	l, s0 := verifyLog(3, 8)
	want := append([]Event(nil), l.Events...)
	head := l.Head()
	arr := &l.Events[0]

	l.RewindTo(s0, len(l.Events))
	if len(l.Events) != 3 || l.VerifyEnd() != 8 {
		t.Fatalf("rewind: len %d window %d, want 3 and 8", len(l.Events), l.VerifyEnd())
	}
	for i := 3; i < 8; i++ {
		got := l.Append(Event{Kind: Damage, Obj: state.ObjID(i), Amount: int32(i), IDs: []state.ObjID{1, state.ObjID(i)}})
		if got.Seq != uint64(i) {
			t.Fatalf("re-executed event %d got Seq %d", i, got.Seq)
		}
	}
	if !l.VerifyClose() || l.VerifyEnd() != 0 {
		t.Fatal("a fully reproduced window must close clean")
	}
	if &l.Events[0] != arr {
		t.Fatal("the rewind re-allocated the live event array")
	}
	for i := range want {
		if string(want[i].Append(nil)) != string(l.Events[i].Append(nil)) {
			t.Fatalf("event %d changed across the re-execution", i)
		}
	}
	if l.Head() != head || l.HeadAt(len(l.Events)) != head {
		t.Fatalf("head %s after the re-execution, want %s", l.Head(), head)
	}
	l.Append(Event{Kind: Note, Text: "past the window"})
	if len(l.Events) != 9 || l.Head() == head {
		t.Fatal("an append past the closed window must extend the log and the chain")
	}
	if l.Head() != l.HeadAt(len(l.Events)) {
		t.Fatal("chain desynced from HeadAt after the window")
	}
}

// TestRewindToCatchesADivergence: a re-executed event that differs from the
// recorded one panics with LogDivergence before anything is written.
func TestRewindToCatchesADivergence(t *testing.T) {
	l, s0 := verifyLog(2, 5)
	rec := l.Events[3]
	l.RewindTo(s0, len(l.Events))
	l.Append(Event{Kind: Damage, Obj: 2, Amount: 2, IDs: []state.ObjID{1, 2}})
	func() {
		defer func() {
			p := recover()
			err, ok := p.(error)
			var dv LogDivergence
			if !ok || !errors.As(err, &dv) || dv.Index != 3 {
				t.Fatalf("want a LogDivergence at 3, got %v", p)
			}
		}()
		l.Append(Event{Kind: Damage, Obj: 3, Amount: 99})
	}()
	if string(l.Events[:4][3].Append(nil)) != string(rec.Append(nil)) {
		t.Fatal("the diverging append overwrote the recorded event")
	}
}

// TestRewindToShortWindowReportsUnreproduced: a re-execution that stops
// before the window's end reports it on close.
func TestRewindToShortWindowReportsUnreproduced(t *testing.T) {
	l, s0 := verifyLog(2, 5)
	l.RewindTo(s0, len(l.Events))
	l.Append(Event{Kind: Damage, Obj: 2, Amount: 2, IDs: []state.ObjID{1, 2}})
	if l.VerifyClose() {
		t.Fatal("a window closed short must report it")
	}
}

// TestRewindToLeavesAClonesSharedPrefix: a clone taken at the posed point
// shares the live log's prefix; the live log's rewind and re-execution never
// write into it, and the clone's own later append diverges cleanly.
func TestRewindToLeavesAClonesSharedPrefix(t *testing.T) {
	l, s0 := verifyLog(3, 6)
	c := l.Clone()
	head := c.Head()
	l.RewindTo(s0, len(l.Events))
	for i := 3; i < 6; i++ {
		l.Append(Event{Kind: Damage, Obj: state.ObjID(i), Amount: int32(i), IDs: []state.ObjID{1, state.ObjID(i)}})
	}
	l.VerifyClose()
	l.Append(Event{Kind: Note, Text: "live"})
	c.Append(Event{Kind: Note, Text: "clone"})
	if l.Events[6].Text != "live" || c.Events[6].Text != "clone" {
		t.Fatal("the live log and its clone wrote into each other")
	}
	if c.HeadAt(6) != head {
		t.Fatal("the clone's shared prefix changed")
	}
	if c.Clone().VerifyEnd() != 0 {
		t.Fatal("a clone must never carry a verify window")
	}
}
