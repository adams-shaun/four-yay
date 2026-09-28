package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// A hidden-library `ChangeZone` search carrying `ExileFaceDown$ True`
// (CR 701.23 search, CR 701.23d the mandatory quantity-only find, CR 711
// face down) must put the found card into exile FACE DOWN. The
// MoveZone event carries the `exiled_with_face_down` marker (so the face-down
// state survives replay -- CR 711's durable state, never a projection-only
// flag), the exiling source rides the same event's Amount, the object's
// ExiledWith records that source, and the view redacts the face from an
// opponent while its owner may still look at it (CR 708.8).
//
// This is the `applyLibrarySearch` half of the AGENTS.md row that named
// `GainControl$`, `ExileFaceDown$` and `Imprint$` together. The
// `ExileFaceDown$` half was already honoured by the shared
// applyFaceDownMarker call on the library-origin mover (commit 8425e563 added
// it to applyLibrarySearch, 387fbeec folded it into the shared helper), but
// no test drove it through this mover -- facedown_changezone_test.go covers
// the bare FaceDown$ battlefield-entry spelling and foretell_adjacent_test.go
// the hand/stack routes. This test pins the library-search carrier end to end
// against a real compiled corpus card, so a future change that drops the
// marker from applyLibrarySearch fails here.

// librarySearchExileFaceDown resolves a mandatory quantity-only hidden-library
// search and returns the exiled MoveZone events and the surviving decision's
// source. The carrier is Mangara's Tome: its ETB `ChangeZone$` line searches
// the library for five cards, exiles them in a face-down pile, and shuffles.
func librarySearchExileFaceDown(t *testing.T, e *Engine) ([]events.Event, state.ObjID) {
	t.Helper()
	tome := searchMoveByName(t, e, "Mangara's Tome", state.ZBattlefield)
	d := passUntilNonPriority(t, e, 60)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("Mangara's Tome ETB did not reach a hidden-library search; got %+v", d)
	}
	// Precondition for the whole test: this is the mandatory quantity-only
	// find (Min == Max == 5), not a stated-quality "fail to find" search, so
	// the five picks below are the shape CR 701.23d requires.
	if d.Min != 5 || d.Max != 5 {
		t.Fatalf("precondition: Mangara's Tome search offers %d..%d, want 5..5", d.Min, d.Max)
	}
	if len(d.Options) < 5 {
		t.Fatalf("precondition: only %d search options; need at least 5", len(d.Options))
	}
	start := len(e.L.Events)
	submitChoices(t, e, 0, 1, 2, 3, 4)
	for i := 0; i < 40 && !e.G.Over; i++ {
		dd := e.Pending()
		if dd == nil || dd.Kind != decision.KPriority {
			break
		}
		idx := -1
		for _, o := range dd.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", dd)
		}
		if err := e.Submit(decision.Intent{Seq: dd.Seq, Player: dd.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	var exiled []events.Event
	for _, ev := range e.L.Events[start:] {
		// The search's five finds carry the face-down marker; a sibling
		// non-face-down exile (the shuffle-the-pile rider's own move) is not
		// part of this pin.
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.Counter == "exiled_with_face_down" {
			exiled = append(exiled, ev)
		}
	}
	return exiled, tome
}

// TestLibrarySearchExilesFoundCardsFaceDown is the carrier: five found cards
// leave the library face down, each MoveZone stamped with the face-down
// marker and the exiling source, each object state FaceDown with its
// ExiledWith recorded.
func TestLibrarySearchExilesFoundCardsFaceDown(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Mangara's Tome")

	exiled, tome := librarySearchExileFaceDown(t, e)
	if tome == 0 {
		t.Fatal("precondition: Mangara's Tome has no object id")
	}
	if len(exiled) != 5 {
		t.Fatalf("Mangara's Tome exiling search emitted %d exile moves, want 5", len(exiled))
	}

	moved := make([]state.ObjID, 0, len(exiled))
	for _, ev := range exiled {
		// The one event field that carries the face-down exile: without it the
		// replay folds the card into exile face up and the object state below
		// cannot be face down.
		if ev.Counter != "exiled_with_face_down" {
			t.Errorf("exile MoveZone for %d has Counter=%q, want %q", ev.Obj, ev.Counter, "exiled_with_face_down")
		}
		// The exiling source rides Amount, so the object's ExiledWith is
		// recoverable from the event alone (durable state, replay-safe).
		if ev.Amount != int32(tome) {
			t.Errorf("exile MoveZone for %d has Amount=%d, want the exiling source %d", ev.Obj, ev.Amount, tome)
		}
		moved = append(moved, ev.Obj)
	}

	for _, id := range moved {
		o := e.G.Obj(id)
		if o == nil {
			t.Fatalf("exiled object %d vanished", id)
		}
		if o.Zone != state.ZExile {
			t.Fatalf("precondition: object %d zone = %s, want exile", id, o.Zone)
		}
		// The compared values must actually differ: a real printed name proves
		// the face exists and is hidden by FaceDown, not absent.
		if o.Face() == nil || o.Face().Name == "" {
			t.Fatalf("precondition: exiled object %d has no printed face", id)
		}
		if !o.FaceDown {
			t.Errorf("found card %s (%d) is in exile face UP; ExileFaceDown$ was ignored", o.Face().Name, id)
		}
		if o.ExiledWith != tome {
			t.Errorf("found card %s (%d) records ExiledWith=%d, want the exiling source %d", o.Face().Name, id, o.ExiledWith, tome)
		}
	}

	replayCheck(t, e, cfg)
}

// TestLibrarySearchFaceDownExileIsHiddenFromOpponents is the CR 708.8 half:
// the face-down card's face is redacted from a non-owner's view of the exile
// zone, while its owner may still look at it. Without the FaceDown state the
// opponent reads the found card's printed name out of exile.
func TestLibrarySearchFaceDownExileIsHiddenFromOpponents(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := searchEngine(t, reg, "Mangara's Tome")

	exiled, _ := librarySearchExileFaceDown(t, e)
	if len(exiled) == 0 {
		t.Fatal("precondition: no card was exiled")
	}
	wanted := make(map[state.ObjID]string, len(exiled))
	for _, ev := range exiled {
		o := e.G.Obj(ev.Obj)
		if o == nil || o.Face() == nil || o.Face().Name == "" {
			t.Fatalf("precondition: exiled object %d has no printed face", ev.Obj)
		}
		if !o.FaceDown {
			t.Fatalf("precondition: exiled object %d is not face down", ev.Obj)
		}
		wanted[ev.Obj] = o.Face().Name
	}

	// Opponent (viewer 1) must not read any of the names out of seat 0's exile.
	opp := view.Project(e.G, e, 1, nil)
	seen := make(map[state.ObjID]bool, len(exiled))
	for _, cv := range opp.Players[0].Exile {
		name, ok := wanted[cv.ID]
		if !ok {
			continue
		}
		seen[cv.ID] = true
		if !cv.FaceDown {
			t.Errorf("opponent's view of exiled %d lacks the face_down flag", cv.ID)
		}
		if cv.Name == name {
			t.Errorf("opponent's view of exiled %d leaks the printed name %q", cv.ID, cv.Name)
		}
	}
	for id := range wanted {
		if !seen[id] {
			t.Errorf("exiled %d is missing from the opponent's exile projection", id)
		}
	}

	// Owner (viewer 0) may look at its own face-down exiled cards (CR 708.8):
	// the projection must show the real face, so the redaction above is a
	// redaction and not a lost face.
	own := view.Project(e.G, e, 0, nil)
	revealed := make(map[state.ObjID]bool, len(exiled))
	for _, cv := range own.Players[0].Exile {
		if name, ok := wanted[cv.ID]; ok && cv.Name == name && cv.FaceDown {
			revealed[cv.ID] = true
		}
	}
	for id, name := range wanted {
		if !revealed[id] {
			t.Errorf("owner's view of its face-down exiled %s (%d) does not show the printed face", name, id)
		}
	}
}
