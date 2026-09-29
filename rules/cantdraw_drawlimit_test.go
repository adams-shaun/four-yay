package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func cantDrawFixture(t *testing.T, seed uint64, carrier *cards.Card) (*Engine, Config) {
	t.Helper()
	deck := append(mountainDeck(t, 40), carrier)
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

func putCantDrawCarrierOnBattlefield(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := findCardByName(t, e, name)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: e.G.Obj(id).Zone, To: state.ZBattlefield})
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s zone = %v, want battlefield", name, o)
	}
	return id
}

func TestCantDrawDrawLimitCapsPerTurn(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)

	// Narset's Opponent scope caps seat 1 while leaving her controller free.
	narsetCard := mustCorpusCard(t, reg, "Narset, Parter of Veils")
	e, cfg := cantDrawFixture(t, 881, narsetCard)
	putCantDrawCarrierOnBattlefield(t, e, narsetCard.Faces[0].Name)
	if len(e.G.Zone(state.ZLibrary, 1)) < 2 || len(e.G.Zone(state.ZLibrary, 0)) < 1 {
		t.Fatal("precondition: both players need cards available to draw")
	}
	beforeEvents := countDraw(e)
	beforeHand := len(e.G.Zone(state.ZHand, 1))
	emitDraw(t, e, 1)
	if got := countDraw(e) - beforeEvents; got != 1 {
		t.Fatalf("first draw event delta = %d, want 1", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - beforeHand; got != 1 {
		t.Fatalf("first draw hand delta = %d, want 1", got)
	}
	beforeEvents, beforeHand = countDraw(e), len(e.G.Zone(state.ZHand, 1))
	emitDraw(t, e, 1)
	if got := countDraw(e) - beforeEvents; got != 0 {
		t.Fatalf("second draw event delta = %d, want 0 (DrawLimit$ 1)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 1)) - beforeHand; got != 0 {
		t.Fatalf("second draw hand delta = %d, want 0", got)
	}
	controllerHand := len(e.G.Zone(state.ZHand, 0))
	emitDraw(t, e, 0)
	if got := len(e.G.Zone(state.ZHand, 0)) - controllerHand; got != 1 {
		t.Fatalf("Narset controller draw hand delta = %d, want 1 (Opponent scope)", got)
	}
	replayCheck(t, e, cfg)

	// Spirit's Player scope independently caps every player, one at a time.
	spiritCard := mustCorpusCard(t, reg, "Spirit of the Labyrinth")
	e, cfg = cantDrawFixture(t, 882, spiritCard)
	putCantDrawCarrierOnBattlefield(t, e, spiritCard.Faces[0].Name)
	beforeEvents, beforeHand = countDraw(e), len(e.G.Zone(state.ZHand, 1))
	emitDraw(t, e, 1)
	if countDraw(e)-beforeEvents != 1 || len(e.G.Zone(state.ZHand, 1))-beforeHand != 1 {
		t.Fatal("Spirit's first draw for seat 1 was not performed")
	}
	beforeEvents, beforeHand = countDraw(e), len(e.G.Zone(state.ZHand, 1))
	emitDraw(t, e, 1)
	if countDraw(e)-beforeEvents != 0 || len(e.G.Zone(state.ZHand, 1))-beforeHand != 0 {
		t.Fatal("Spirit did not prevent seat 1's second draw")
	}
	replayCheck(t, e, cfg)
}

func TestCR121DrawLimitCap(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	spiritCard := mustCorpusCard(t, reg, "Spirit of the Labyrinth")
	e, _ := cantDrawFixture(t, 883, spiritCard)
	putCantDrawCarrierOnBattlefield(t, e, spiritCard.Faces[0].Name)
	before := countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 1 {
		t.Fatalf("first draw delta = %d, want 1", got)
	}
	before = countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - before; got != 0 {
		t.Fatalf("CR 121.6 second draw delta = %d, want 0", got)
	}
}
