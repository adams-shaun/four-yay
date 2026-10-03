package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The diguntil1 reveal-until tests (api:DigUntil, Forge's DigUntilEffect —
// a DIFFERENT primitive from Dig with its own param family). The ask-side
// leaves use the askHost double (primitives_test.go) whose Ask captures the
// posed decision and suspends — the effects-package stand-in for
// rules.Engine; the no-host leaves use the plain fakeHost, whose Ask
// reports false (the R-9 deterministic decline).

// digUntilFixture builds seat 0's library as [land, aura, land, land] — the
// match is the SECOND card, so a first-card match cannot pass by
// coincidence — plus one more land after the found card, and a Grizzly-Bear
// creature on the battlefield (the bearer the CR 303.4f non-cast Aura entry
// attaches to). Returns (host, ids in library order).
func digUntilFixture(t *testing.T) (*askHost, []state.ObjID) {
	t.Helper()
	h := &askHost{}
	h.g = state.NewGame(names(2))
	bear := mkCard(t, "Name:Bear\nTypes:Creature\nPT:2/2\nOracle:x\n")
	h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{h.g.AddObject(bear, 0).ID})
	land := mkCard(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n")
	aura := mkCard(t, "Name:Halo\nManaCost:W\nTypes:Enchantment Aura\nK:Enchant:Creature\nOracle:x\n")
	ids := []state.ObjID{
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(aura, 0).ID,
		h.g.AddObject(land, 0).ID,
		h.g.AddObject(land, 0).ID,
	}
	h.g.SetZone(state.ZLibrary, 0, ids)
	return h, ids
}

// songbirdsSA is Songbirds' Blessing's compiled param shape: reveal until an
// Aura, the found card MAY go to the battlefield, the decline goes to the
// hand (OptionalNoDestination$), the revealed rest to the bottom.
const songbirdsSA = "SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | OptionalNoDestination$ Hand | RevealedDestination$ Library | RevealedLibraryPosition$ -1"

// TestDigUntilRevealsUntilTheMatchMovesFoundAndRest is the core leaf: the
// public reveal names every card turned over INCLUDING the found one, the
// found card lands in FoundDestination$, the revealed rest land in
// RevealedDestination$ at RevealedLibraryPosition$ -1 (the bottom), and the
// cards after the found card never move.
func TestDigUntilRevealsUntilTheMatchMovesFoundAndRest(t *testing.T) {
	h, ids := digUntilFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | RevealedDestination$ Library | RevealedLibraryPosition$ -1"))
	if h.asked != nil {
		t.Fatalf("a mandatory found move must not ask: %+v", h.asked)
	}
	// The public reveal: non-Secret, to the library's owner, every card
	// turned over including the found one (the first two; the scan stopped
	// at the match, so the tail was never turned over).
	var reveal *events.Event
	for i := range h.log {
		e := &h.log[i]
		if e.Kind == events.Note && len(e.IDs) == 2 && !e.Secret {
			reveal = e
			break
		}
	}
	if reveal == nil || reveal.Player != 0 || reveal.IDs[0] != ids[0] || reveal.IDs[1] != ids[1] {
		t.Fatalf("reveal Note = %+v, want a public 2-id note [land, aura] to seat 0", reveal)
	}
	// The found card is on the battlefield, attached to the Bear (the CR
	// 303.4f non-cast-entry attach's deterministic stand-in); the Bear is the
	// first battlefield permanent.
	bearer := h.g.Zone(state.ZBattlefield, 0)[0]
	if o := h.g.Obj(ids[1]); o.Zone != state.ZBattlefield || o.AttachedTo != bearer {
		t.Fatalf("found card zone/attach = %s/%d, want battlefield/%d", o.Zone, o.AttachedTo, bearer)
	}
	// The revealed rest (the first land) is at the BOTTOM of the library;
	// the cards after the found card keep their order on top.
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 3 || lib[0] != ids[2] || lib[1] != ids[3] || lib[2] != ids[0] {
		t.Fatalf("library = %v, want [%d %d %d] — tail untouched, revealed rest at the bottom", lib, ids[2], ids[3], ids[0])
	}
	if o := h.g.Obj(ids[2]); o.Zone != state.ZLibrary || h.g.Obj(ids[3]).Zone != state.ZLibrary {
		t.Fatal("cards after the found card left the library")
	}
}

// TestDigUntilDefaultDestinationsFollowTheRevealedPile pins the defaults:
// FoundDestination$ absent = the found card is one of the revealed cards and
// goes where the pile goes (Forge's reading; every one of the 25 corpus lines
// without FoundDestination$ says so in its Oracle text), RevealedDestination$
// absent = Library with no RevealedLibraryPosition$ = the stay-in-place
// default (no events -- the revealed cards were the top cards and stay on top
// in their existing order). The found card used to default to Hand, which
// handed Consuming Aberration's victim the land it should have milled.
func TestDigUntilDefaultDestinationsFollowTheRevealedPile(t *testing.T) {
	h, ids := digUntilFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura"))
	if h.asked != nil {
		t.Fatalf("a mandatory found move must not ask: %+v", h.asked)
	}
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 4 || lib[0] != ids[0] || lib[1] != ids[1] || lib[2] != ids[2] || lib[3] != ids[3] {
		t.Fatalf("library = %v, want %v untouched -- found card and revealed rest stay in place", lib, ids)
	}
	for _, e := range h.log {
		if e.Kind == events.MoveZone {
			t.Fatalf("a card moved with no destinations given: %+v", e)
		}
	}

	// RevealedDestination$ Graveyard alone (Consuming Aberration, Balustrade
	// Spy, Mind Funeral): the found card goes to the graveyard WITH the rest;
	// the cards after it stay in the library.
	h, ids = digUntilFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura | RevealedDestination$ Graveyard"))
	for _, id := range ids[:2] {
		if o := h.g.Obj(id); o.Zone != state.ZGraveyard {
			t.Fatalf("revealed card %d zone = %s, want graveyard (the found card follows RevealedDestination$)", id, o.Zone)
		}
	}
	for _, id := range ids[2:] {
		if o := h.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("card %d after the found one left the library: %s", id, o.Zone)
		}
	}

	// An explicit FoundDestination$ still wins.
	h, ids = digUntilFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Hand | RevealedDestination$ Graveyard"))
	if o := h.g.Obj(ids[1]); o.Zone != state.ZHand {
		t.Fatalf("found card zone = %s, want hand (explicit FoundDestination$)", o.Zone)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZGraveyard {
		t.Fatalf("revealed rest zone = %s, want graveyard", o.Zone)
	}
}

// TestDigUntilNoHostDeclinesToTheRevealedPile is the R-9 no-host stand-in
// for the corpus's OptionalFoundMove$ carriers that carry NO
// OptionalNoDestination$ (Genesis Storm, Hei Bai, Aurora Awakener): the
// decline sends the found card to the revealed pile — the oracles say
// "then put all cards revealed this way that weren't put onto the
// battlefield on the bottom".
func TestDigUntilNoHostDeclinesToTheRevealedPile(t *testing.T) {
	fh, ids := digUntilFixture(t)
	h := &fakeHost{g: fh.g} // a host that cannot ask: the deterministic decline
	Resolve(h, &Ctx{Controller: 0}, sa(t, "SP$ DigUntil | Valid$ Aura | FoundDestination$ Battlefield | OptionalFoundMove$ True | RevealedDestination$ Library | RevealedLibraryPosition$ -1"))
	// The found card joins the revealed pile at the bottom (after the first
	// land); the library's tail is untouched.
	lib := h.g.Zone(state.ZLibrary, 0)
	if len(lib) != 4 || lib[0] != ids[2] || lib[1] != ids[3] || lib[2] != ids[0] || lib[3] != ids[1] {
		t.Fatalf("library = %v, want [%d %d %d %d] — decline joined the bottom pile after the revealed rest", lib, ids[2], ids[3], ids[0], ids[1])
	}
}

// TestDigUntilKetriaRememberFoundFeedsTheChainedMove pins RememberFound$ on
// the REAL corpus card Ketria (RolledChaos: DB$ DigUntil | Valid$
// Permanent.nonLand | FoundDestination$ Exile | RevealedDestination$ Exile |
// RememberFound$ True | SubAbility$ DBChoose): both piles go to exile, the
// found card joins the resolution's Remembered, and the chained
// DBChangeZone (Defined$ Remembered) then moves it onto the battlefield —
// the found card's final zone IS the Remembered discipline's observable
// trace.
func TestDigUntilKetriaRememberFoundFeedsTheChainedMove(t *testing.T) {
	_, ketria := corpusSA(t, "Ketria", "RolledChaos")
	if ketria.API != "DigUntil" {
		t.Fatalf("Ketria RolledChaos API = %q, want DigUntil", ketria.API)
	}
	h, ids := digUntilFixture(t)
	// Ketria's spec is Permanent.nonLand — a permanent CARD away from the
	// battlefield — so the ISLAND does not match and the AURA (ids[1], a
	// non-land card) does; the found card is the second card.
	c := &Ctx{Controller: 0}
	Resolve(h, c, ketria)
	// The revealed non-found card (ids[0]) is in exile; the two tail cards
	// never moved.
	if o := h.g.Obj(ids[0]); o.Zone != state.ZExile {
		t.Fatalf("revealed card %d zone = %s, want exile (RevealedDestination$ Exile)", ids[0], o.Zone)
	}
	for _, id := range []state.ObjID{ids[2], ids[3]} {
		if o := h.g.Obj(id); o.Zone != state.ZLibrary {
			t.Fatalf("tail card %d zone = %s, want library", id, o.Zone)
		}
	}
	// The chained ChangeZone (Defined$ Remembered, Origin$ Exile,
	// Destination$ Battlefield) moved the Remembered found card onto the
	// battlefield — RememberFound$ feeding the sub-ability, end to end.
	if o := h.g.Obj(ids[1]); o.Zone != state.ZBattlefield {
		t.Fatalf("Remembered found card zone = %s, want battlefield via the chained Defined$ Remembered move", o.Zone)
	}
}
