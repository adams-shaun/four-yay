package searchprobe

import (
	"bytes"
	"testing"

	"github.com/adams-shaun/gorge/view"
)

// TestObservationStripsOwnLibraryFromFrames pins the omission that starved the
// sampler: view.PlayerView gained an own-library CONTENTS list
// (own_library_list), view.Project fills it with the real cards, and the
// observation frame carried it -- raw engine ObjIDs and all. The recorded and
// the hypothetical engine allocate hidden library objects from different
// arenas, so every sampled world was rejected at frame 0 on "board state", and
// the unordered contents also told the search which cards it exists to sample.
// The collector's noPotentialChars Chars declares SuppressOwnLibrary, so the
// projection never builds it -- the same treatment PotentialActions and
// OwnDeck get.
//
// Two things must hold at once, or the strip is either missing or vacuous:
//
//   - the PROJECTION still carries a non-empty own library (the view fact this
//     ticket exposes is real and the fixture exercises it), and
//   - the CAPTURED FRAME carries no `library` member for any seat, while a
//     sibling hidden zone the frame IS allowed to know (the actor's own hand)
//     is present, so "no library" cannot be an all-empty board.
func TestObservationStripsOwnLibraryFromFrames(t *testing.T) {
	e := observationEngine(t, 17)
	// Precondition: the projected seat view must actually carry a non-empty
	// own library, or the strip is asserted against nothing. Reach it through
	// the same entry the collector uses.
	v := view.Project(e.G, e, 0, e.Pending())
	if v.Players[0].ID != 0 {
		t.Fatalf("fixture viewer is not seat 0: got %d", v.Players[0].ID)
	}
	if len(v.Players[0].Library) == 0 {
		t.Fatal("fixture projects no own library; the strip below would be vacuous")
	}

	c := NewCollector(0)
	frame, err := c.Capture(e, e.L.Events)
	if err != nil {
		t.Fatal(err)
	}
	board := frame.Board
	if !bytes.Contains(board, []byte(`"hand"`)) {
		t.Fatal(`frame board has no "hand" member: the fixture board is empty and the library assertion proves nothing`)
	}
	if bytes.Contains(board, []byte(`"library"`)) {
		t.Fatalf("observation frame carries the own-library contents:\n%s", board)
	}
}
