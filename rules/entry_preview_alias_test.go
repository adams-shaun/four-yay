package rules

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEntryPreviewLogIsAPrivateFork pins entryPreview's shared-prefix log:
// the preview reads the live events in place, but an event appended to the
// preview -- even while the live log's array has spare capacity -- lands in
// a private array and never reaches the live log, and a live append never
// shows in the preview.
func TestEntryPreviewLogIsAPrivateFork(t *testing.T) {
	c, diags := cards.ParseBytes("preview.txt", []byte("Name:Preview Bear\nTypes:Creature\nPT:2/2\n"))
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	c.Link()
	deck := make([]*cards.Card, 20)
	for i := range deck {
		deck[i] = c
	}
	e := New(Config{Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}})
	e.L.Reserve(len(e.L.Events) + 64) // spare capacity a naive share would write into
	var id state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		if ids := e.G.Zone(z, 0); len(ids) > 0 {
			id = ids[0]
			break
		}
	}
	if id == 0 {
		t.Fatal("precondition: no card to preview")
	}
	from := e.G.Obj(id).Zone
	before := append([]events.Event(nil), e.L.Events...)
	head := e.L.Head()
	preview, entrant := e.entryPreview(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
	if entrant != id || preview.G.Obj(id).Zone != state.ZBattlefield || e.G.Obj(id).Zone != from {
		t.Fatalf("preview did not isolate the entry: entrant=%d preview zone=%v live zone=%v", entrant, preview.G.Obj(id).Zone, e.G.Obj(id).Zone)
	}
	if len(preview.L.Events) != len(before) || !reflect.DeepEqual(preview.L.Events, before) {
		t.Fatal("preview log does not start as the live log")
	}
	preview.L.Append(events.Event{Kind: events.Note, Text: "preview only"})
	if len(e.L.Events) != len(before) || !reflect.DeepEqual(e.L.Events, before) || e.L.Head() != head {
		t.Fatal("a preview append reached the live log")
	}
	if spare := e.L.Events[:len(e.L.Events)+1]; spare[len(before)].Text == "preview only" {
		t.Fatal("a preview append wrote into the live log's spare capacity")
	}
	e.L.Append(events.Event{Kind: events.Note, Text: "live only"})
	if got := preview.L.Events[len(before)].Text; got != "preview only" {
		t.Fatalf("a live append reached the preview log: %q", got)
	}
}
