package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The WithTotalCMC$ budget end-to-end pin on Protean Hulk's REAL compiled
// corpus card: "When Protean Hulk dies, search your library for any number
// of creature cards with total mana value 6 or less, put them onto the
// battlefield, then shuffle." Protean Hulk is in NO repo deck and NO legacy
// golden deck (grepping internal/testutil/decks/*.json for protean_hulk
// returns nothing), so no chain head depends on this card.
//
// The deck is built from compiled corpus cards only (the
// search_library_test.go convention), so no Forge script text is committed
// here either.

// hulkWindowEngine puts Protean Hulk on seat 0's battlefield and orders seat
// 0's library to a known window whose creatures span the budget boundary:
//
//	[Polar Kraken 11, Hill Giant 4, Grizzly Bears 2, Craw Wurm 6,
//	 Grizzly Bears 2, Forest (land, ineligible), Serra Angel 5]
//
// Under a budget of 6 the budget-eligible set is {Hill Giant, Bear, Bear,
// Craw Wurm, Serra Angel} -- the 11-MV Kraken is individually unaffordable
// -- and the forced greedy take (Hill Giant + a Bear, sum 6) cannot consume
// all five, so a real ask is posed.
func hulkWindowEngine(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, []state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	windowCards := []*cards.Card{
		searchCorpusCard(t, reg, "Polar Kraken"),  // 11 MV: over budget
		searchCorpusCard(t, reg, "Hill Giant"),    // 4 MV
		searchCorpusCard(t, reg, "Grizzly Bears"), // 2 MV
		searchCorpusCard(t, reg, "Craw Wurm"),     // 6 MV
		searchCorpusCard(t, reg, "Grizzly Bears"), // 2 MV
		forest,                                  // land: ineligible
		searchCorpusCard(t, reg, "Serra Angel"), // 5 MV
	}
	deck := make([]*cards.Card, 0, 40)
	for i := 0; i < 7; i++ {
		deck = append(deck, forest)
	}
	deck = append(deck, windowCards...)
	deck = append(deck, searchCorpusCard(t, reg, "Protean Hulk"))
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"hulk", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 7 {
		t.Fatalf("library = %d cards, want at least the 7-card window", len(lib))
	}
	// The deck is shuffled at New, so pull the seven window cards back into
	// the library with logged moves, then set the whole library order with a
	// LibraryOrder event -- both replay-safe, unlike a raw SetZone. Protean
	// Hulk is excluded from the window and sits below it.
	takeFromLibrary := func(name string) state.ObjID {
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
			for _, id := range e.G.Zone(z, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
					if z == state.ZHand {
						e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
					}
					return id
				}
			}
		}
		t.Fatalf("window card %q not in seat 0 hand/library", name)
		return 0
	}
	window := make([]state.ObjID, 0, len(windowCards))
	for _, c := range windowCards {
		window = append(window, takeFromLibrary(c.Faces[0].Name))
	}
	inWindow := make(map[state.ObjID]bool, len(window))
	for _, id := range window {
		inWindow[id] = true
	}
	newLib := append([]state.ObjID(nil), window...)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if !inWindow[id] {
			newLib = append(newLib, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: newLib})
	// Protean Hulk on the battlefield (a logged fixture move; the trigger
	// under test is the death, so entry machinery is not exercised here).
	hulkID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Protean Hulk" {
			hulkID = id
		}
	}
	if hulkID == 0 {
		t.Fatal("Protean Hulk not found in seat 0's library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: hulkID, From: state.ZLibrary, To: state.ZBattlefield})
	return e, cfg, window
}
