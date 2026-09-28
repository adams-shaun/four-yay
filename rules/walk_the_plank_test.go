package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// wtpEngine builds a two-seat fixture where seat 0 holds Walk the Plank (real
// corpus card) and seat 1 holds the named opposing creatures (also real corpus
// cards). Returns (engine, walk-the-plank id, opposing creature ids in deck
// order). This is the fixture shape rules/activation_limit_test.go and
// rules/scry_surveil_test.go use: New(Config{Decks: [][]*cards.Card{...}}) with
// real corpus cards, then moveByName onto the battlefield and addMana for the
// caster's pool.
func wtpEngine(t *testing.T, opponents ...string) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	wtp, ok := reg.Lookup("Walk the Plank")
	if !ok {
		t.Fatal("corpus fixture: Walk the Plank missing")
	}
	deck0 := []*cards.Card{wtp}
	deck0 = append(deck0, mountainDeck(t, 39)...)

	var deck1 []*cards.Card
	for _, name := range opponents {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture: %q missing", name)
		}
		deck1 = append(deck1, c)
	}
	deck1 = append(deck1, mountainDeck(t, 40-len(deck1))...)

	e := New(Config{Seed: 42, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck0, deck1},
		Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)

	// Walk the Plank must be castable this turn, so it must sit in seat 0's
	// hand; if it is still in the library, move it there with a logged event.
	var wtpID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Walk the Plank" {
			wtpID = id
		}
	}
	if wtpID == 0 {
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Walk the Plank" {
				wtpID = id
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
				break
			}
		}
	}
	if wtpID == 0 {
		t.Fatal("Walk the Plank not in seat 0's hand or library")
	}

	var opponentIDs []state.ObjID
	for _, name := range opponents {
		opponentIDs = append(opponentIDs, moveByName(t, e, 1, name, state.ZBattlefield))
	}
	return e, wtpID, opponentIDs
}

// wtpCastIndex returns the index of the "cast" option for id in the current
// priority decision, or -1 if the spell is not offered (e.g. no legal target).
func wtpCastIndex(t *testing.T, e *Engine, id state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			return o.Index
		}
	}
	return -1
}

// TestWalkThePlankOffersAndDestroysNonMerfolkTarget is the rules leaf 1:
// with {B}{B} available and one Merfolk and one non-Merfolk creature opposing,
// the cast IS offered, the target ask offers EXACTLY the non-Merfolk (never
// the Merfolk), and resolution destroys it.
func TestWalkThePlankOffersAndDestroysNonMerfolkTarget(t *testing.T) {
	t.Parallel()
	e, wtpID, opp := wtpEngine(t, "Merfolk of the Pearl Trident", "Grizzly Bears")
	addMana(t, e, 0, "BB")

	idx := wtpCastIndex(t, e, wtpID)
	if idx < 0 {
		t.Fatalf("Walk the Plank not offered despite a legal non-Merfolk target (the Bear)")
	}
	submitChoices(t, e, idx)

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision after announcing Walk the Plank, got %+v", d)
	}
	if len(d.Options) != 1 {
		t.Fatalf("target options = %d (%v), want exactly the single non-Merfolk creature", len(d.Options), d.Options)
	}
	if d.Options[0].Obj != opp[1] {
		t.Fatalf("offered target %v, want the Bear %v (the Merfolk must never be offered)", d.Options[0].Obj, opp[1])
	}
	submitChoices(t, e, d.Options[0].Index)

	passUntilStackEmpty(t, e, 20)

	if z := e.G.Obj(opp[1]).Zone; z != state.ZGraveyard {
		t.Fatalf("the non-Merfolk target was not destroyed: zone %v, want Graveyard", z)
	}
	if z := e.G.Obj(opp[0]).Zone; z != state.ZBattlefield {
		t.Fatalf("the Merfolk was not supposed to be destroyed but is off the battlefield: zone %v", z)
	}
}

// TestWalkThePlankWithheldWhenOnlyMerfolkToTarget is the rules leaf 2: with
// only a Merfolk opposing, the cast is NOT offered (CR 601.2c — a spell with
// no legal target may not be announced).
func TestWalkThePlankWithheldWhenOnlyMerfolkToTarget(t *testing.T) {
	t.Parallel()
	e, wtpID, _ := wtpEngine(t, "Merfolk of the Pearl Trident")
	addMana(t, e, 0, "BB")

	if idx := wtpCastIndex(t, e, wtpID); idx >= 0 {
		t.Fatalf("Walk the Plank offered with only a Merfolk to target (CR 601.2c: no legal target)")
	}
}
