package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
)

// trigBearSrc carries one printed trigger, so a TriggerPush naming it mints
// a real face-less ability object.
const trigBearSrc = "Name:Trig bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\n" +
	"T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ x\n" +
	"SVar:TrigDraw:DB$ Draw | NumCards$ 1\nOracle:x\n"

// TestDerivedSeqSpansPushesAndObjectLocalEvents pins derived_transparent.go's
// push and object-local admissions: under local effects a TriggerPush (the
// arena grows by one face-less ability object) keeps derivedSeq and the next
// walk is served the earlier entry; a CounterChange on a non-source keeps it
// too but retires that object's entry, which re-derives with its counter;
// a CounterChange on an effect's source moves derivedSeq. derivedMemoVerify
// (on in this binary) recomputes every served entry.
func TestDerivedSeqSpansPushesAndObjectLocalEvents(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	lord := onBoard(t, e, 0, "Name:Lord\nManaCost:2\nTypes:Creature Lord\nPT:1/1\nS:Mode$ Continuous | Affected$ Creature.Other+YouCtrl | AddPower$ 1 | Description$ x\nOracle:x\n")
	bear := onBoard(t, e, 0, trigBearSrc)
	walk := func() int32 {
		e.beginDerivedMemo()
		defer e.endDerivedMemo()
		return e.Derived(bear).Power
	}
	if p := walk(); p != 3 {
		t.Fatalf("bear power %d, want 3", p)
	}
	seq := e.derivedSeq
	objs := len(e.G.Objs)
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: bear, Amount: 0})
	if len(e.G.Objs) != objs+1 {
		t.Fatal("precondition: the TriggerPush minted no ability object")
	}
	if p := walk(); p != 3 {
		t.Fatalf("bear power after the push %d, want 3", p)
	}
	if e.derivedSeq != seq {
		t.Fatalf("a face-less TriggerPush under local effects moved derivedSeq %d -> %d", seq, e.derivedSeq)
	}
	if m := e.derivedMemo.at(bear); m == nil || m.seq != seq {
		t.Fatal("the bear's entry was not served across the push")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	if p := walk(); p != 4 {
		t.Fatalf("bear power after its +1/+1 counter %d, want 4 (stale entry served)", p)
	}
	if e.derivedSeq != seq {
		t.Fatalf("a counter on a non-source moved derivedSeq %d -> %d", seq, e.derivedSeq)
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: lord, Counter: "P1P1", Amount: 1})
	_ = walk()
	if e.derivedSeq == seq {
		t.Fatal("a counter on an effect's source kept derivedSeq")
	}
}

// TestSBAQuietSpansAPush pins sbaquiet.go's push admission: a TriggerPush
// after a quiet pass loop keeps the skip (the arena grew by exactly the one
// face-less ability object it minted).
func TestSBAQuietSpansAPush(t *testing.T) {
	t.Parallel()
	e := newSeats(t, 2)
	bear := onBoard(t, e, 0, trigBearSrc)
	e.emit(events.Event{Kind: events.Note, Text: "settle"})
	e.checkStateBased()
	if !e.sbaQuietNow() {
		t.Fatal("precondition: a pass loop that applied nothing should arm the quiet key")
	}
	e.emit(events.Event{Kind: events.TriggerPush, Player: 0, Obj: bear, Amount: 0})
	if !e.sbaQuietNow() {
		t.Fatal("a face-less TriggerPush should keep the quiet key")
	}
	e.emit(events.Event{Kind: events.Scry, Player: 0})
	if !e.sbaQuietNow() {
		t.Fatal("a pure marker should keep the quiet key")
	}
}
