package events

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// CloneIntoFrom copies only the history a recycled array lacks when the
// array's provenance is the source's own array, copies everything otherwise,
// and leaves the array zero past the copied history either way.
func TestLogCloneIntoFromReusesTheProvenPrefix(t *testing.T) {
	root := NewLog(7)
	for i := 0; i < 40; i++ {
		root.Append(Event{Kind: Note, Text: "root", Obj: state.ObjID(i)})
	}
	spare := make([]Event, 0, 400)
	c := root.CloneInto(spare, nil)
	for i := 0; i < 30; i++ {
		c.Append(Event{Kind: Note, Text: "world", IDs: []state.ObjID{state.ObjID(i)}})
	}
	from, n := c.Provenance()
	if from != &root.Events[0] || n != 40 {
		t.Fatalf("provenance %p/%d, want the root's array and 40", from, n)
	}
	recycled, dirty := c.Events[:cap(c.Events)], len(c.Events)
	// The root grows in place: the recycled prefix is still its history.
	for i := 0; i < 10; i++ {
		root.Append(Event{Kind: Note, Text: "more"})
	}
	if &root.Events[0] != from {
		t.Fatal("fixture: the root's array moved")
	}
	recycled[0].Text = "sentinel" // a copy would overwrite this; reuse keeps it
	d := root.CloneIntoFrom(recycled, from, n, dirty, nil)
	if d.Events[0].Text != "sentinel" {
		t.Fatal("the proven prefix was copied again")
	}
	recycled[0].Text = "root"
	if !reflect.DeepEqual(d.Events, root.Events) || d.Head() != root.Head() {
		t.Fatal("the reused clone's history differs from the root's")
	}
	for i := len(d.Events); i < dirty; i++ {
		if !reflect.DeepEqual(recycled[i], Event{}) {
			t.Fatalf("stale slot %d past the history was not zeroed", i)
		}
	}
	// Another source's provenance copies everything.
	other := NewLog(7)
	for i := 0; i < 20; i++ {
		other.Append(Event{Kind: Note, Text: "other"})
	}
	recycled[0].Text = "sentinel"
	e := other.CloneIntoFrom(recycled, from, n, len(d.Events), nil)
	if !reflect.DeepEqual(e.Events, other.Events) || e.Head() != other.Head() {
		t.Fatal("a foreign provenance was trusted")
	}
	for i := len(e.Events); i < len(d.Events); i++ {
		if !reflect.DeepEqual(recycled[i], Event{}) {
			t.Fatalf("stale slot %d past the foreign history was not zeroed", i)
		}
	}
}
