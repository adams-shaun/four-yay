package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The twopiles1 end-to-end pin on Fact or Fiction's REAL compiled corpus
// card: the spell is cast, the top five are revealed by the (already
// implemented) PeekAndReveal arm, the chained DB$ TwoPiles splits them —
// the separator's KChoose over the five, then the chooser's two-option pile
// pick — and the chosen pile lands in the caster's hand while the other
// lands in the graveyard, entirely through the existing ChangeZone path.
// Before twopiles1 this card resolved as the loud-but-harmless
// "unimplemented API TwoPiles" Note and moved nothing. Fact or Fiction is in
// NO repo deck and NO legacy golden deck (the filing report's "Fae Dominion
// deck-carrier" premise is FALSE — measured against
// internal/testutil/decks/*.json), so no chain head depends on this card.
//
// The helpers come from search_library_test.go / cast_test.go (same
// package); the deck is built from compiled corpus cards only, so no Forge
// script text is committed here either.

// fofTestEngine deals seat 0 a 40-card deck whose first six distinct cards
// are Fact or Fiction plus the five reveal-window cards, reorders seat 0's
// library to a KNOWN exact order (the five reveal cards on top, in order),
// adds 3U+ to the pool, casts Fact or Fiction, and drives to the split ask.
// Returns the engine, config, the five reveal ids in reveal order, and the
// FoF id.
func fofTestEngine(t *testing.T) (*Engine, Config, []state.ObjID, state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	fof := searchCorpusCard(t, reg, "Fact or Fiction")
	reveal := []*cards.Card{}
	for _, name := range []string{"Grizzly Bears", "Forest", "Mountain", "Island", "Swamp"} {
		reveal = append(reveal, searchCorpusCard(t, reg, name))
	}
	forest := reveal[1]
	mountain := reveal[2]
	deck := append([]*cards.Card{fof}, reveal...)
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, searchCorpusCard(t, reg, "Grizzly Bears"))
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9304, Names: []string{"fof", "separator"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	fofID := searchMoveByName(t, e, "Fact or Fiction", state.ZHand)
	// The five reveal cards must be IN the library: if the opening hand took
	// one, put it back.
	lib := e.G.Zone(state.ZLibrary, 0)
	revealIDs := make([]state.ObjID, 0, 5)
	for _, name := range []string{"Grizzly Bears", "Forest", "Mountain", "Island", "Swamp"} {
		id := state.ObjID(0)
		for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
			for _, cand := range e.G.Zone(z, 0) {
				if o := e.G.Obj(cand); o != nil && o.Face() != nil && o.Face().Name == name {
					id = cand
					break
				}
			}
			if id != 0 {
				break
			}
		}
		if id == 0 {
			t.Fatalf("reveal card %q absent from library/hand", name)
		}
		if e.G.Obj(id).Zone == state.ZHand {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
		revealIDs = append(revealIDs, id)
	}
	lib = e.G.Zone(state.ZLibrary, 0)
	order := append([]state.ObjID(nil), revealIDs...)
	for _, id := range lib {
		if !fofContainsID(revealIDs, id) {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order})
	addMana(t, e, 0, "UUUU")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == fofID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Fact or Fiction: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "twopiles_split" {
		t.Fatalf("after the cast: %+v, want the TwoPiles split KChoose", d)
	}
	return e, cfg, revealIDs, fofID
}

func fofContainsID(ids []state.ObjID, want state.ObjID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestFactOrFictionSplitsAndMovesThePiles is the before/after pin: before
// the fix the five cards stayed in the library behind the
// "unimplemented API TwoPiles" Note; after it, pile A lands in the caster's
// hand and pile B in the graveyard, and the log replays byte-identically.
func TestFactOrFictionSplitsAndMovesThePiles(t *testing.T) {
	t.Parallel()
	e, cfg, revealIDs, fofID := fofTestEngine(t)
	d := e.Pending()
	if d.Player != 1 {
		t.Fatalf("split ask player = %d, want the separator (opponent) 1", d.Player)
	}
	if d.Min != 0 || d.Max != 5 || len(d.Options) != 5 {
		t.Fatalf("split bounds/options = %d..%d, %d options, want 0..5 over the five revealed", d.Min, d.Max, len(d.Options))
	}
	for i, o := range d.Options {
		if o.Obj != revealIDs[i] {
			t.Fatalf("split option %d = %d, want %d (reveal order)", i, o.Obj, revealIDs[i])
		}
	}
	// Pile A = the first and third revealed cards.
	start := len(e.L.Events)
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "twopiles_pick" {
		t.Fatalf("after the split: %+v, want the TwoPiles pile pick", d)
	}
	if d.Player != 0 {
		t.Fatalf("pick ask player = %d, want the chooser (Defined$ You) 0", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "pile-a" || d.Options[1].Kind != "pile-b" {
		t.Fatalf("pick options = %+v, want pile-a then pile-b", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index) // the caster takes pile A
	for _, id := range []state.ObjID{revealIDs[0], revealIDs[2]} {
		if z := e.G.Obj(id).Zone; z != state.ZHand {
			t.Fatalf("chosen-pile card %d zone = %s, want hand", id, z)
		}
	}
	for _, id := range []state.ObjID{revealIDs[1], revealIDs[3], revealIDs[4]} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("unchosen-pile card %d zone = %s, want graveyard", id, z)
		}
	}
	if z := e.G.Obj(fofID).Zone; z != state.ZGraveyard {
		t.Fatalf("Fact or Fiction zone = %s, want the graveyard after resolving", z)
	}
	// Fact or Fiction's oracle text instructs NO shuffle: the pile bodies are
	// DB$ ChangeZone | Origin$ Library movements, and the shared
	// moveDefinedLibraryObjects tail would otherwise fire its default
	// CR 701.23d search shuffle once per pile body, destroying the order of
	// the caster's remaining library (the same hazard for Sphinx of Uthuun,
	// Steam Augury, Epiphany at the Drownyard, Jace Architect of Thought,
	// Intrude on the Mind, Unesh — none of which shuffle either). The pile
	// bodies run with NoShuffle forced on, so the whole resolution emits no
	// events.Shuffle at all.
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle {
			t.Fatalf("TwoPiles resolution emitted a Shuffle for player %d; Fact or Fiction never shuffles: %+v", ev.Player, e.L.Events[start:])
		}
	}
	replayCheck(t, e, cfg)
}

// TestFactOrFictionPickBGivesTheOtherPile pins the reversed pick on the real
// card: the chooser takes pile B, so the OTHER pile's cards go to hand.
func TestFactOrFictionPickBGivesTheOtherPile(t *testing.T) {
	t.Parallel()
	e, cfg, revealIDs, _ := fofTestEngine(t)
	d := e.Pending()
	submitChoices(t, e, d.Options[0].Index, d.Options[2].Index)
	d = e.Pending()
	submitChoices(t, e, d.Options[1].Index) // pile B
	for _, id := range []state.ObjID{revealIDs[1], revealIDs[3], revealIDs[4]} {
		if z := e.G.Obj(id).Zone; z != state.ZHand {
			t.Fatalf("pile-B card %d zone = %s, want hand", id, z)
		}
	}
	for _, id := range []state.ObjID{revealIDs[0], revealIDs[2]} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("pile-A card %d zone = %s, want graveyard", id, z)
		}
	}
	replayCheck(t, e, cfg)
}
