package effects

import (
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// hostlessSearchRegistryOnce shares ONE corpus decode across the four
// hostless-search leaves (and the fixture helpers), so pinning the R-9
// stand-in does not compound the corpus load a fresh worktree's budget can
// ill afford. Same pattern as rules/search_library_test.go's
// librarySearchRegistryOnce.
var (
	hostlessSearchRegistryOnce sync.Once
	hostlessSearchRegistry     *cards.Registry
)

func hostlessSearchTestRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	hostlessSearchRegistryOnce.Do(func() {
		hostlessSearchRegistry = testutil.CorpusRegistry(t)
	})
	if hostlessSearchRegistry == nil {
		t.Skip("library-search corpus unavailable")
	}
	return hostlessSearchRegistry
}

// searchFixtureNoHost is a 2-seat effect board for the hidden-library
// ChangeZone R-9 stand-in. It places `source` (carrying the search SA) in
// seat 0's hand and seat 0's library to the ordered `lib` cards, so the
// search's deterministic eligible list (and therefore the option list the
// decision would have built) is exactly `lib` order for a bare ChangeType$
// Card search. Returns the host, the source id and the library object ids in
// that same order.
func searchFixtureNoHost(t *testing.T, source *cards.Card, lib ...*cards.Card) (*fakeHost, state.ObjID, []state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	src := h.g.AddObject(source, 0)
	ids := make([]state.ObjID, 0, len(lib))
	for _, c := range lib {
		o := h.g.AddObject(c, 0)
		ids = append(ids, o.ID)
	}
	h.g.SetZone(state.ZHand, 0, []state.ObjID{src.ID})
	h.g.SetZone(state.ZLibrary, 0, ids)
	h.g.Obj(src.ID).Zone = state.ZHand
	for _, id := range ids {
		h.g.Obj(id).Zone = state.ZLibrary
	}
	return h, src.ID, ids
}

// countSearchMoves reports the number of library MoveZone events and the
// number of seat-0 shuffles in a host's event log.
func countSearchMoves(h *fakeHost) (moves, shuffles int) {
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone {
			moves++
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	return
}

// TestLibrarySearchNoHostQuantityFindsDeterministicFirst pins CR 701.23d on
// the R-9 stand-in: a hostless quantity-only search (Demonic Tutor's
// ChangeType$ Card) must find exactly Min (1) card, and that card must be the
// deterministic FIRST eligible card in the decision's own option order -- not
// just any card, and not zero. Before srch1 the stand-in found nothing; the
// pre-fix no-host lay the quantity-only shape down as 0 cards, which is the
// answer the decision it bypassed would itself have refused.
func TestLibrarySearchNoHostQuantityFindsDeterministicFirst(t *testing.T) {
	reg := hostlessSearchTestRegistry(t)
	tutor, ok := reg.Lookup("Demonic Tutor")
	if !ok {
		t.Fatal("missing corpus Demonic Tutor")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("missing corpus Forest")
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("missing corpus Mountain")
	}
	bear, ok := reg.Lookup("Grizzly Bears")
	if !ok {
		t.Fatal("missing corpus Grizzly Bears")
	}

	h, src, ids := searchFixtureNoHost(t, tutor, forest, mountain, bear)
	first, second, third := ids[0], ids[1], ids[2]
	// The deterministic eligible list, in library order: Forest, Mountain,
	// Grizzly Bears. Demonic Tutor changes to the hand (Destination$ Hand).
	Resolve(h, &Ctx{Source: src, Controller: 0}, tutor.Faces[0].Abilities[0])

	// Exactly Min (1) card moved, and it is the first eligible card.
	if h.g.Obj(first).Zone != state.ZHand {
		t.Fatalf("first eligible card (%d, Forest) not moved to hand, zone=%s", first, h.g.Obj(first).Zone)
	}
	if h.g.Obj(second).Zone != state.ZLibrary || h.g.Obj(third).Zone != state.ZLibrary {
		t.Fatalf("stand-in moved more than Min cards: Mountain=%s Grizzly=%s", h.g.Obj(second).Zone, h.g.Obj(third).Zone)
	}
	moves, shuffles := countSearchMoves(h)
	if moves != 1 || shuffles != 1 {
		t.Fatalf("moves=%d shuffles=%d, want 1/1: %+v", moves, shuffles, h.log)
	}
	// The Note names what the stand-in supplied, and it is deterministic.
	hostlessNote := false
	for _, ev := range h.log {
		if ev.Kind == events.Note && ev.Obj == src && ev.Text == "finds 1 card(s) (no engine host to ask)" {
			hostlessNote = true
		}
	}
	if !hostlessNote {
		t.Fatalf("no-host quantity search emitted no hostless note: %+v", h.log)
	}
	if h.Suspended() {
		t.Fatal("no-host quantity search suspended")
	}
}

// TestLibrarySearchNoHostQuantityTakesAllWhenFewerThanMin pins the "never
// wedge" half of the same rule: a quantity-only search whose requested
// quantity exceeds the eligible count takes ALL of them (701.23d's "as many
// as possible"). Manipulate Fate is ChangeType$ Card, ChangeNum$ 3; the
// fixture library holds only two eligible cards, so Min/Max clamp to 2 and
// the stand-in must move both, not panic and not dangle.
func TestLibrarySearchNoHostQuantityTakesAllWhenFewerThanMin(t *testing.T) {
	reg := hostlessSearchTestRegistry(t)
	mfmt, ok := reg.Lookup("Manipulate Fate")
	if !ok {
		t.Fatal("missing corpus Manipulate Fate")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("missing corpus Forest")
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("missing corpus Mountain")
	}

	h, src, ids := searchFixtureNoHost(t, mfmt, forest, mountain)
	Resolve(h, &Ctx{Source: src, Controller: 0}, mfmt.Faces[0].Abilities[0])

	for _, id := range ids {
		if h.g.Obj(id).Zone != state.ZExile {
			t.Fatalf("eligible card %d not exiled, zone=%s", id, h.g.Obj(id).Zone)
		}
	}
	moves, shuffles := countSearchMoves(h)
	if moves != 2 || shuffles != 1 {
		t.Fatalf("moves=%d shuffles=%d, want 2/1: %+v", moves, shuffles, h.log)
	}
	if h.Suspended() {
		t.Fatal("no-host quantity search suspended instead of taking all")
	}
}

func TestLibrarySearchNoHostFindsNothingAndShuffles(t *testing.T) {
	reg := hostlessSearchTestRegistry(t)
	wilds, ok := reg.Lookup("Evolving Wilds")
	if !ok {
		t.Fatal("missing corpus Evolving Wilds")
	}
	forest, ok := reg.Lookup("Forest")
	if !ok {
		t.Fatal("missing corpus Forest")
	}
	h := newHost(t, 2)
	src := h.g.AddObject(wilds, 0)
	basic := h.g.AddObject(forest, 0)
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{basic.ID})
	h.g.Obj(src.ID).Zone = state.ZBattlefield
	h.g.Obj(basic.ID).Zone = state.ZLibrary

	var searchSA = wilds.Faces[0].Abilities[0]
	Resolve(h, &Ctx{Source: src.ID, Controller: 0}, searchSA)

	if h.g.Obj(basic.ID).Zone != state.ZLibrary {
		t.Fatalf("no-host stand-in moved the basic to %s, want library", h.g.Obj(basic.ID).Zone)
	}
	moves, shuffles := 0, 0
	for _, ev := range h.log {
		if ev.Kind == events.MoveZone {
			moves++
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if moves != 0 || shuffles != 1 {
		t.Fatalf("no-host search emitted %d moves and %d shuffles, want 0/1: %+v", moves, shuffles, h.log)
	}
	if h.Suspended() {
		t.Fatal("no-host search suspended")
	}
}
