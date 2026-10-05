package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// gravestormEngine builds a 2-seat game whose seat-0 deck is bracketed by the
// named corpus cards: the Gravestorm carrier plus a Grizzly Bears that the
// test kills through a real MoveZone. Both seats are padded with Mountains so
// the game is playable; the toss is advanced to seat 0 by seatZeroStart, which
// returns the effective Config for replayCheck.
func gravestormEngine(t *testing.T, reg *cards.Registry, carrier string) (*Engine, Config) {
	t.Helper()
	spell := mustCorpusCard(t, reg, carrier)
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	deck := append([]*cards.Card{spell, bear}, mountainDeck(t, 38)...)
	cfg := seatZeroStart(Config{Seed: 9202, Names: []string{"caster", "opponent"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// gravestormCast picks the cast option for id, answers the target decision
// with the offered player option for want, then drains the stack. The cast
// flow's pending target snapshot is built at priority time, so the mana must
// already be funded before this is called (addMana does that).
func gravestormCast(t *testing.T, e *Engine, id state.ObjID, want state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %d: %+v", id, d.Options)
	}
	submitChoices(t, e, idx)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == want {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat %d not offered as a target: %+v", want, d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 60)
}

// copiesResolvedIntoExileThenCeased counts the copies this game made. Under
// CR 707.10a/704.5e a copy of a non-permanent spell is exiled when it resolves
// (CR 608.2m/111.7) and then ceases to exist as a state-based action, so the
// object is observed either resting in exile or already ceased; both mean the
// copy was created and left the stack.
func copiesInExile(e *Engine) int {
	n := 0
	for _, o := range e.G.Objs {
		if o.IsCopy && (o.Zone == state.ZExile || o.Zone == state.ZCeased) {
			n++
		}
	}
	return n
}

// TestOminousHarvestGravestormCopiesPerDeath is the end-to-end CR 702.84 pin
// on the real corpus card Ominous Harvest ("Gravestorm -- when you cast this
// spell, copy it for each permanent put into a graveyard from the battlefield
// this turn. Target player draws a card and loses 1 life."). One Grizzly
// Bears is put into the graveyard from the battlefield earlier in the turn,
// so the cast makes original + one copy; each drains the targeted player 1
// life through the card's own DB$ LoseLife sub-ability (the copy keeps the
// original's target -- the documented copy-target stand-in). A control run
// with no death this turn makes no copy and drains 1.
func TestOminousHarvestGravestormCopiesPerDeath(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)

	run := func(t *testing.T, deadThisTurn bool) (lost int32, copies int) {
		e, cfg := gravestormEngine(t, reg, "Ominous Harvest")
		harvest := mustCorpusCard(t, reg, "Ominous Harvest")
		if deadThisTurn {
			// Move the seeded Grizzly Bears from the library to the
			// battlefield, then kill it through the real MoveZone path.
			bear := mustCorpusCard(t, reg, "Grizzly Bears")
			bf := moveSeededCard(t, e, 0, bear, state.ZBattlefield)
			e.emit(events.Event{Kind: events.MoveZone, Obj: bf, From: state.ZBattlefield, To: state.ZGraveyard})
		}
		h := moveSeededCard(t, e, 0, harvest, state.ZHand)
		addMana(t, e, 0, "BBG") // {2}{B}: two generic covered by G+G
		life := e.G.Players[1].Life
		gravestormCast(t, e, h, 1)
		replayCheck(t, e, cfg)
		return life - e.G.Players[1].Life, copiesInExile(e)
	}

	lost, copies := run(t, true)
	if lost != 2 {
		t.Errorf("one permanent died this turn: seat 1 lost %d life, want 2 (original + one copy)", lost)
	}
	if copies != 1 {
		t.Errorf("one permanent died this turn: %d copies created, want 1", copies)
	}

	lost, copies = run(t, false)
	if lost != 1 {
		t.Errorf("nothing died this turn: seat 1 lost %d life, want 1", lost)
	}
	if copies != 0 {
		t.Errorf("nothing died this turn: %d copies created, want 0", copies)
	}
}
