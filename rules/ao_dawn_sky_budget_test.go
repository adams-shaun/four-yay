package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The WithTotalCMC$ budget end-to-end pin on Ao, the Dawn Sky's REAL compiled
// corpus card: "When Ao, the Dawn Sky dies, choose one — Look at the top
// seven cards of your library. Put any number of nonland permanent cards with
// total mana value 4 or less from among them onto the battlefield. Put the
// rest on the bottom of your library in a random order." Ao is in NO repo
// deck and NO legacy golden deck (grepping internal/testutil/decks/*.json for
// ao_the_dawn_sky returns nothing), so no chain head depends on this card.
//
// The deck is built from compiled corpus cards only (the
// search_library_test.go convention), so no Forge script text is committed
// here either.

// aoWindowEngine puts Ao on seat 0's battlefield, orders seat 0's library to
// a known exact top window whose nonland permanents span the budget boundary,
// and returns (engine, config, window ids in library order). The window is:
//
//	[Air Elemental 5, Hill Giant 4, Grizzly Bears 2, Serra Angel 5,
//	 Grizzly Bears 2, Forest (land, ineligible), Craw Wurm 6]
//
// Under a budget of 4 the budget-eligible set is {Hill Giant 4, Bear 2,
// Bear 2} — the 5- and 6-MV permanents are individually unaffordable — and
// the forced greedy take (Hill Giant alone, since 4+2 > 4) cannot consume all
// three, so a real ask is posed.
func aoWindowEngine(t *testing.T, reg *cards.Registry, seed uint64) (*Engine, Config, []state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	// The opening deal takes deck[0..6], so seven Forests go into the hand and
	// the library's top seven -- what Ao's DigNum$ 7 reveals at Main1 of the
	// play (the starting seat skips its turn-1 draw) -- are exactly deck[7..13]:
	// the window. Ao sits at deck[14], below the window.
	windowCards := []*cards.Card{
		searchCorpusCard(t, reg, "Air Elemental"), // 5 MV
		searchCorpusCard(t, reg, "Hill Giant"),    // 4 MV
		searchCorpusCard(t, reg, "Grizzly Bears"), // 2 MV
		searchCorpusCard(t, reg, "Serra Angel"),   // 5 MV
		searchCorpusCard(t, reg, "Grizzly Bears"), // 2 MV
		forest,                                // land: ineligible
		searchCorpusCard(t, reg, "Craw Wurm"), // 6 MV
	}
	deck := make([]*cards.Card, 0, 40)
	for i := 0; i < 7; i++ {
		deck = append(deck, forest)
	}
	deck = append(deck, windowCards...)
	deck = append(deck, searchCorpusCard(t, reg, "Ao, the Dawn Sky"))
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = forest
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"ao", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) < 7 {
		t.Fatalf("library = %d cards, want at least the 7-card window", len(lib))
	}
	// The deck is shuffled at New, so pull the seven window cards back into
	// the library with logged moves (a hand card moves in first), then set
	// the whole library order with a LibraryOrder event -- both replay-safe,
	// unlike a raw SetZone. Ao is excluded from the window and stays below it.
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
	// Ao on the battlefield (a logged fixture move; the trigger under test is
	// the death, so entry machinery is not exercised here).
	aoID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Ao, the Dawn Sky" {
			aoID = id
		}
	}
	if aoID == 0 {
		t.Fatal("Ao not found in seat 0's library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: aoID, From: state.ZLibrary, To: state.ZBattlefield})
	return e, cfg, window
}
