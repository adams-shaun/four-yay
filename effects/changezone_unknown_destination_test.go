package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// destNoteText returns the single unsupported-destination Note's text and
// whether exactly one Note was emitted.
func destNoteText(h *fakeHost) (string, int) {
	var notes []events.Event
	for _, e := range h.log {
		if e.Kind == events.Note {
			notes = append(notes, e)
		}
	}
	if len(notes) == 0 {
		return "", 0
	}
	return notes[0].Text, len(notes)
}

// TestChangeZoneUnknownDestinationFailsClosed drives effChangeZone with a
// nonempty Destination$ the zone vocabulary does not model (Ante,
// PlanarDeck). The card must stay exactly where it was -- no graveyard move,
// no move at all -- and the resolution must emit one deterministic Note
// naming the unsupported text. The control cases prove the same board and
// script shape DO move for a recognized destination, so the "no move" half
// cannot pass on a dead/no-op effect.
func TestChangeZoneUnknownDestinationFailsClosed(t *testing.T) {
	cases := []struct {
		name     string
		dest     string
		wantNote string
		wantZone state.Zone
	}{
		{"ante", "Ante", "unsupported ChangeZone Destination$ Ante", state.ZBattlefield},
		{"planardeck", "PlanarDeck", "unsupported ChangeZone Destination$ PlanarDeck", state.ZBattlefield},
		{"recognized-exile-control", "Exile", "", state.ZExile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, c := fixtureHost(t)
			src := h.g.Obj(c.Source)
			// Precondition: the object is in the modelled origin the script
			// names, so the origin parser cannot mask the destination.
			h.Emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
			if got := h.g.Obj(c.Source).Zone; got != state.ZBattlefield {
				t.Fatalf("precondition: source not on battlefield, zone = %v", got)
			}
			sa := &cards.SA{API: "ChangeZone", Params: map[string]string{
				"Defined":     "Self",
				"Origin":      "Battlefield",
				"Destination": tc.dest,
			}}
			effChangeZone(h, c, sa)

			got := h.g.Obj(c.Source).Zone
			if got != tc.wantZone {
				t.Fatalf("after ChangeZone destination %q: zone = %v, want %v", tc.dest, got, tc.wantZone)
			}
			text, n := destNoteText(h)
			if tc.wantNote == "" {
				if n != 0 {
					t.Fatalf("recognized destination emitted Note(s): %d, first %q", n, text)
				}
				return
			}
			if n != 1 {
				t.Fatalf("want exactly one unsupported-destination Note, got %d: %+v", n, h.log)
			}
			if text != tc.wantNote {
				t.Fatalf("Note.Text = %q, want %q", text, tc.wantNote)
			}
		})
	}
}
