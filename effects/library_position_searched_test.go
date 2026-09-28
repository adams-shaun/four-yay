package effects

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The searched-library / Defined$-fetch placement helper placeLibraryObjects
// (effects/zone.go) used to read only the literal spellings "0" and "-1" of
// LibraryPosition$ and return SILENTLY on everything else, so Long-Term
// Plans ("Search your library for a card, then shuffle and put that card
// third from the top", LibraryPosition$ 2) shuffled the found card and then
// left it at the BOTTOM of the library with no diagnostic at all. These
// tests pin the fix: a non-{0,-1} value resolves through the same
// NumResolved grammar placeTargetedLibraryObjects applies, places via
// libraryOrderPlacementAt, and an unresolvable value degrades LOUDLY (one
// Note, the MoveZone bottom append stands).

// searchedBoard lays seat 0's library out as [S, F0 .. F3] -- the searched
// card first (so the deterministic no-host stand-in finds exactly it), four
// fillers behind it, enough cards that "third from the top" (index 2)
// differs from both the top (index 0) and the bottom (index 4). It returns
// the host and the searched card's object id.
func searchedBoard(t *testing.T, h *fakeHost, searchedID state.ObjID) {
	t.Helper()
	lib := append([]state.ObjID{searchedID}, lzFills(t, h, 4)...)
	h.g.SetZone(state.ZLibrary, 0, lib)
	for _, id := range lib {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	if got := h.g.Zone(state.ZLibrary, 0); len(got) != 5 {
		t.Fatalf("precondition: library holds %d cards, want 5 so index 2 is neither top nor bottom", len(got))
	}
}

// TestPlaceLibraryObjectsNonZeroPosition drives the helper directly with
// LibraryPosition$ 2 (the Long-Term Plans shape): the moved card must come
// to rest third from the top, exactly one Secret LibraryOrder event, no
// Note. NumResolved resolves the bare signed literal with an empty Ctx, so
// this is the shape the corpus card hits without any Ctx state.
func TestPlaceLibraryObjectsNonZeroPosition(t *testing.T) {
	h := newHost(t, 2)
	fillers := lzFills(t, h, 4)
	searched := lzCreature(t, h, 0, state.ZBattlefield, "Searched")
	if z := h.g.Obj(searched).Zone; z != state.ZBattlefield {
		t.Fatalf("precondition: searched card zone = %v, want battlefield", z)
	}
	// Mimic the post-Move state: the card is already in its owner's library
	// (MoveZone appended it at the bottom) and the helper places it.
	h.g.SetZone(state.ZLibrary, 0, append([]state.ObjID{fillers[0], fillers[1], fillers[2], fillers[3], searched}))
	h.g.Obj(searched).Zone = state.ZLibrary
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 5 || lib[len(lib)-1] != searched {
		t.Fatalf("precondition: library = %v, want 5 cards with the searched card appended at the bottom", lib)
	}

	src := lzAdd(t, h, 0, state.ZHand, mkCard(t, "Name:Placer\nTypes:Instant\nOracle:x\n"))
	sa := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Library | LibraryPosition$ 2")
	placeLibraryObjects(h, &Ctx{Source: src, Controller: 0}, sa, 0, []state.ObjID{searched}, state.ZLibrary)

	lib = h.g.Zone(state.ZLibrary, 0)
	want := []state.ObjID{fillers[0], fillers[1], searched, fillers[2], fillers[3]}
	if !slices.Equal(lib, want) {
		t.Fatalf("library = %v, want the searched card third from the top: %v", lib, want)
	}
	var orders int
	for _, e := range h.log {
		if e.Kind == events.LibraryOrder {
			orders++
		}
	}
	if orders != 1 {
		t.Fatalf("LibraryOrder events = %d, want exactly 1 (the placement)", orders)
	}
	for _, e := range h.log {
		if e.Kind == events.Note {
			t.Fatalf("unexpected Note %+v (a resolvable position must not be loud)", e)
		}
	}
}

// TestLongTermPlansPutsCardThirdFromTop is the corpus-driven end-to-end pin
// on the live carrier: the ACTUAL compiled Long-Term Plans spell
// (SP$ ChangeZone | Origin$ Library | Destination$ Library |
// LibraryPosition$ 2 | ChangeType$ Card | ChangeNum$ 1 | Mandatory$ True)
// driven through Resolve on a plain fakeHost. The search takes the first
// eligible card (the searched card sits first in the library, so the
// deterministic no-host stand-in finds exactly it), the mandatory-shuffle
// arm shuffles and hands the tail to placeLibraryObjects, and the card must
// end up at index 2 of the post-shuffle library -- third from the top, not
// the bottom the pre-fix helper left it at.
func TestLongTermPlansPutsCardThirdFromTop(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ltp, ok := reg.Lookup("Long-Term Plans")
	if !ok {
		t.Fatal("corpus has no Long-Term Plans")
	}
	f := ltp.Faces[0]
	if len(f.Abilities) == 0 || f.Abilities[0].API != "ChangeZone" {
		t.Fatal("Long-Term Plans has no ChangeZone spell ability in the corpus")
	}
	spell := f.Abilities[0]
	if spell.Params["LibraryPosition"] != "2" || spell.Params["Origin"] != "Library" ||
		spell.Params["Destination"] != "Library" {
		t.Fatalf("compiled spell params = %v, want Origin$ Library / Destination$ Library / LibraryPosition$ 2", spell.Params)
	}

	h := newHost(t, 2)
	ltpID := lzAdd(t, h, 0, state.ZHand, ltp)
	h.g.SetZone(state.ZHand, 0, []state.ObjID{ltpID})
	searched := lzCreature(t, h, 0, state.ZLibrary, "Searched")
	searchedBoard(t, h, searched)

	Resolve(h, &Ctx{Source: ltpID, Controller: 0}, spell)

	if z := h.g.Obj(searched).Zone; z != state.ZLibrary {
		t.Fatalf("searched card zone = %v, want library (the spell moved nothing)", z)
	}
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 5 {
		t.Fatalf("library holds %d cards, want all 5 (one searched, none lost)", len(lib))
	}
	if lib[2] != searched {
		t.Fatalf("searched card landed at index %d of %v, want index 2 (third from the top, not the bottom)", slices.Index(lib, searched), lib)
	}
	var orders int
	for _, e := range h.log {
		if e.Kind == events.LibraryOrder && e.Player == 0 {
			orders++
		}
	}
	if orders != 1 {
		t.Fatalf("seat-0 LibraryOrder events = %d, want exactly 1 (the placement the fix adds): %+v", orders, h.log)
	}
	for _, e := range h.log {
		if e.Kind == events.Note && strings.Contains(e.Text, "LibraryPosition$") {
			t.Fatalf("unexpected LibraryPosition$ Note %+v (a resolvable position must not be loud)", e)
		}
	}
}

// TestPlaceLibraryObjectsUnresolvablePositionIsLoud pins the fail-closed
// direction on the searched-library path: a LibraryPosition$ value the Num
// grammar cannot resolve emits ONE loud Note and the MoveZone bottom append
// stands -- the card is never silently placed by a guessed position, and
// the placement itself never runs.
func TestPlaceLibraryObjectsUnresolvablePositionIsLoud(t *testing.T) {
	h := newHost(t, 2)
	lzFills(t, h, 4)
	searched := lzCreature(t, h, 0, state.ZLibrary, "Searched")
	if lib := h.g.Zone(state.ZLibrary, 0); len(lib) != 5 || lib[len(lib)-1] != searched {
		t.Fatalf("precondition: library = %v, want 5 cards with the searched card at the bottom", lib)
	}
	src := lzAdd(t, h, 0, state.ZHand, mkCard(t, "Name:Placer\nTypes:Instant\nOracle:x\n"))
	sa := sa(t, "DB$ ChangeZone | Origin$ Library | Destination$ Library | LibraryPosition$ TriggeredLKI")
	placeLibraryObjects(h, &Ctx{Source: src, Controller: 0}, sa, 0, []state.ObjID{searched}, state.ZLibrary)

	lib := h.g.Zone(state.ZLibrary, 0)
	if lib[len(lib)-1] != searched {
		t.Fatalf("library = %v, want the searched card still at the BOTTOM (the MoveZone append stands)", lib)
	}
	var notes []string
	var orders int
	for _, e := range h.log {
		if e.Kind == events.Note && strings.Contains(e.Text, "LibraryPosition$") {
			notes = append(notes, e.Text)
		}
		if e.Kind == events.LibraryOrder {
			orders++
		}
	}
	if len(notes) != 1 {
		t.Fatalf("LibraryPosition$ Notes = %v, want exactly one loud diagnostic", notes)
	}
	if orders != 0 {
		t.Fatalf("LibraryOrder events = %d, want 0 (an unresolvable value must not place)", orders)
	}
}
