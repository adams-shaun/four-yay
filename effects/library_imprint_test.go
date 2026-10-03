package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The hidden-library ChangeZone mover `applyLibrarySearch` ignored `Imprint$`:
// a search that found a card (Distant Memories' "search your library for a
// card, exile it", Jace, Architect of Thought's -8, Grim Reminder's reveal)
// did NOT record the moved card in the source's persistent imprintedCards
// association, so the follow-up `Defined$ Imprinted` sub-ability resolved
// nothing and the whole chain silently did the wrong thing.
//
// CR 701.23 (search and move): the found card is the search's result; Forge's
// ChangeZoneEffect records every card it moved through `Imprint$ True` in the
// source card's imprintedCards list, which a later ability reads back with
// `Defined$ Imprinted`. That association is durable state, so it must ride an
// `events.Imprint` (folded by events.Apply, replayed byte-identically), never
// a rules-side field written around the event fold.
//
// These tests drive the real compiled corpus SA through the real mover. The
// reverted-hunk failure is in the report; each test asserts its own
// preconditions (the card really sits in the searched library, the SA really
// carries Imprint$ True) so a vacuous setup fails loudly.

// imprintSearchSA loads a corpus card's first ability and asserts it is the
// hidden-library search carrying Imprint$ True this test is about.
func imprintSearchSA(t *testing.T, card string) (*cards.Card, *cards.SA) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Skip("library-imprint corpus unavailable")
	}
	c, ok := reg.Lookup(card)
	if !ok {
		t.Fatalf("missing corpus %s", card)
	}
	sa := c.Faces[0].Abilities[0]
	if sa.API != "ChangeZone" || sa.Params["Origin"] != "Library" ||
		sa.Params["Imprint"] != "True" {
		t.Fatalf("%s SA = api=%s origin=%q imprint=%q, want the corpus hidden-library search with Imprint$ True",
			card, sa.API, sa.Params["Origin"], sa.Params["Imprint"])
	}
	return c, sa
}

// imprintSearchBoard seeds a two-seat game with `src` under seat 0 and one
// library card under seat 0, returning the host, the ctx and the found card's
// id. The precondition the mover's move reads -- the candidate is IN seat 0's
// library and owned by seat 0 -- is asserted here.
func imprintSearchBoard(t *testing.T, src *cards.Card, victim *cards.Card) (*fakeHost, *Ctx, state.ObjID) {
	t.Helper()
	h := newHost(t, 2)
	s := h.g.AddObject(src, 0)
	found := h.g.AddObject(victim, 0)
	h.g.Obj(found.ID).Zone = state.ZLibrary
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{found.ID})
	if got := h.g.Obj(found.ID).Zone; got != state.ZLibrary {
		t.Fatalf("precondition: found card zone=%s, want library", got)
	}
	c := &Ctx{Source: s.ID, Controller: 0}
	return h, c, found.ID
}

// imprintEventCards returns the ids carried by the plain (imprintedCards)
// events.Imprint, i.e. the ones with empty Text. The exiled-with association
// rides its own Text and is deliberately excluded: it is a DIFFERENT list.
func imprintEventCards(evs []events.Event, source state.ObjID) []state.ObjID {
	var out []state.ObjID
	for _, ev := range evs {
		if ev.Kind == events.Imprint && ev.Obj == source && ev.Text == "" {
			out = append(out, ev.IDs...)
		}
	}
	return out
}

// TestLibrarySearchExileImprintIsRetained is the Distant Memories carrier:
// `Origin$ Library | Destination$ Exile | Imprint$ True`. The found card is
// exiled and recorded in the source's imprintedCards list, so the literal
// follow-up `Defined$ Imprinted` resolves it.
func TestLibrarySearchExileImprintIsRetained(t *testing.T) {
	srcCard, sa := imprintSearchSA(t, "Distant Memories")
	victimCard := mkCard(t, "Name:Found Card\nTypes:Sorcery\nOracle:x\n")
	h, c, found := imprintSearchBoard(t, srcCard, victimCard)

	// The found card must leave the library for the destination this test
	// asserts the imprint of.
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{found})
	applyLibrarySearch(h, c, sa, ChangeZoneOf(sa), 0, state.ZExile, []state.ObjID{found}, []state.Zone{state.ZLibrary})

	o := h.g.Obj(found)
	if o == nil || o.Zone != state.ZExile {
		t.Fatalf("precondition: found card zone=%v, want exile after the search", o.Zone)
	}
	// The association is durable game state on the SOURCE, not a local.
	src := h.g.Obj(c.Source)
	if src == nil {
		t.Fatal("precondition: the search source vanished")
	}
	if !containsID(src.Imprinted, found) {
		t.Fatalf("source Imprinted=%v, want it to contain the exiled find %d", src.Imprinted, found)
	}
	if got := imprintEventCards(h.log, c.Source); len(got) != 1 || got[0] != found {
		t.Fatalf("plain events.Imprint ids = %v, want [%d] on source %d", got, found, c.Source)
	}
	// The whole point of retaining it: the follow-up `Defined$ Imprinted`
	// resolves the moved card.
	def := Defined(h, c, &cards.SA{Params: map[string]string{"Defined": "Imprinted"}})
	if len(def) != 1 || def[0].Obj != found {
		t.Fatalf("Defined$ Imprinted = %+v, want the exiled find %d", def, found)
	}
}

// TestLibrarySearchNonExileImprintIsRetained is the Grim Reminder carrier:
// `Origin$ Library | Destination$ Library | Imprint$ True`. The found card
// never leaves the library, so it is the move (not a zone change to exile)
// that Imprint$ records -- proving the read is not accidentally tied to the
// pre-existing exiled-with association. Forge's imprintedCards list is
// destination-independent, so the durable association must exist here too.
//
// Note the separate, pre-existing CR 607.2a contract: the `Defined$
// Imprinted` READER is deliberately exile-only (imprintAssociationContains),
// so it does not resolve a library-resident imprint. That reader is not what
// this row is about; the retained association is.
func TestLibrarySearchNonExileImprintIsRetained(t *testing.T) {
	srcCard, sa := imprintSearchSA(t, "Grim Reminder")
	if sa.Params["Destination"] != "Library" {
		t.Fatalf("precondition: Grim Reminder destination=%q, want Library (a non-exile Imprint$ carrier)", sa.Params["Destination"])
	}
	victimCard := mkCard(t, "Name:Found Card\nTypes:Instant\nOracle:x\n")
	h, c, found := imprintSearchBoard(t, srcCard, victimCard)

	applyLibrarySearch(h, c, sa, ChangeZoneOf(sa), 0, state.ZLibrary, []state.ObjID{found}, []state.Zone{state.ZLibrary})

	o := h.g.Obj(found)
	if o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("precondition: found card zone=%v, want it still in the library", o.Zone)
	}
	src := h.g.Obj(c.Source)
	if src == nil {
		t.Fatal("precondition: the search source vanished")
	}
	if !containsID(src.Imprinted, found) {
		t.Fatalf("source Imprinted=%v, want it to contain the library find %d", src.Imprinted, found)
	}
	if got := imprintEventCards(h.log, c.Source); len(got) != 1 || got[0] != found {
		t.Fatalf("plain events.Imprint ids = %v, want [%d] on source %d", got, found, c.Source)
	}
}
