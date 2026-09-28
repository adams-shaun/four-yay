package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The ExileFromHand<N/All> cost ticket. Forge's `All` type slot on an Exile
// cost names the WHOLE zone rather than a card filter: Herigast, Erupting
// Nullkite's cast trigger is `Cost$ ExileFromHand<1/All> | Defined$ You |
// NumCards$ 3` -- "you may exile your hand. If you do, draw three cards".
// Before this ticket `All` reached matchesSpec as a filter base word no card
// carries, so the candidate list was empty, triggeredCostComponentsPayable
// judged the component unpayable, and the pay/decline window was offered
// DECLINE-ONLY: the player could never exile their hand and therefore never
// drew the three. The fix reads the spec through isWholeZoneExileSpec at the
// triggered offer gate, the triggered settle walk, and the cast/activation
// gate and payment (defensive -- no cast/activation carrier exists today).
//
// All fixtures are the real compiled corpus card; Herigast is not in any repo
// deck, so TestHeads does not depend on its behaviour changing.

// herigastHandEngine seeds Herigast plus the given filler cards into seat 0's
// deck, draws them into the hand at the main phase, and funds nine green mana
// through real ManaAdd events -- all via logged events, so replayCheck can
// rebuild the game from the log alone. Returns the Config and the ids of the
// hand cards in zone order (Herigast first).
func herigastHandEngine(t *testing.T, filler ...*cards.Card) (*Engine, Config, []state.ObjID) {
	t.Helper()
	herigast := corpusAlternativeCard(t, "Herigast, Erupting Nullkite")
	deck := append([]*cards.Card{herigast}, filler...)
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := seatZeroStart(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck, mountainDeck(t, 40)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	// Clear the drawn opening hand to the library so the fixture hand is
	// exactly Herigast plus the requested junk (each move logged, so replay
	// reproduces it).
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
	}
	e.pending = nil

	ids := []state.ObjID{moveSeededToHand(t, e, 0, herigast.Faces[0].Name)}
	for range filler {
		ids = append(ids, moveNamedLibraryCardToHand(t, e, 0, discardCostJunkName))
	}
	e.pending = nil
	addMana(t, e, 0, "GGGGGGGGG")
	return e, cfg, ids
}

// moveLibraryCardToHand moves the first card named name from seat p's LIBRARY
// to its hand with a logged event. Unlike moveSeededToHand it never returns a
// card already in hand, so repeated calls pull distinct duplicate copies out
// of the library.
func moveNamedLibraryCardToHand(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZLibrary, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
			e.pending = nil
			return id
		}
	}
	t.Fatalf("no more library copies of %q for seat %d", name, p)
	return 0
}

// discardCostJunkName is discardCostJunk's printed name (the const lives in
// discard_cost_test.go and is a full script).
const discardCostJunkName = "Discard Fodder"

// castHerigastAndOpenWindow casts Herigast from hand (the pending cast option
// for its object) and drives until the cast trigger's pay/decline window is
// pending, returning that decision.
func castHerigastAndOpenWindow(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected the main-phase priority, got %+v", d)
	}
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" {
			if obj := e.G.Obj(o.Obj); obj != nil && obj.Face() != nil && obj.Face().Name == "Herigast, Erupting Nullkite" {
				cast = o.Index
			}
		}
	}
	if cast < 0 {
		t.Fatalf("Herigast was not castable with 9 mana: %+v", d.Options)
	}
	submitChoices(t, e, cast)
	d = passUntilNonPriority(t, e, 40)
	return d
}

// TestHerigastExileFromHandAllIsPayableAndExilesWholeHand pins the fix end to
// end: casting Herigast offers its `Cost$ ExileFromHand<1/All>` body as
// PAYABLE (not the decline-only list the defect produced), paying exiles
// EVERY card in the hand -- not one -- and the Draw 3 body runs.
func TestHerigastExileFromHandAllIsPayableAndExilesWholeHand(t *testing.T) {
	t.Parallel()
	junk := card(t, discardCostJunk)
	// Herigast + three junk cards: four cards in hand when the trigger's
	// window opens (the spell has already left the hand for the stack).
	e, cfg, _ := herigastHandEngine(t, junk, junk, junk)

	// Precondition: the hand holds Herigast + three junk, so "whole hand"
	// (4 cards minus the 1 exiled when Herigast hits the stack = 3) is
	// strictly more than any single-card subset the broken filter might
	// have admitted.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 4 {
		t.Fatalf("fixture hand = %d cards, want 4 (Herigast + 3 junk)", got)
	}

	d := castHerigastAndOpenWindow(t, e)
	pay, decline := triggerCostWindowAskDecision(t, d)
	if pay < 0 {
		t.Fatalf("Herigast's ExileFromHand<1/All> body was offered DECLINE-ONLY: %+v", d.Options)
	}
	_ = decline

	handBefore := len(e.G.Zone(state.ZHand, 0))
	if handBefore < 2 {
		t.Fatalf("precondition: hand at the window = %d cards, want >= 2 so the whole-zone reading differs from a one-card pick", handBefore)
	}
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	mark := len(e.L.Events)

	submitChoices(t, e, pay)
	passUntilStackEmpty(t, e, 40)

	// Every hand card was exiled; then the body drew three. The net hand is
	// handBefore - handBefore + 3 = 3.
	if got := len(e.G.Zone(state.ZHand, 0)); got != 3 {
		t.Fatalf("hand after pay = %d, want 3 (whole hand exiled, then 3 drawn)", got)
	}
	exiles := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZExile {
			exiles++
		}
	}
	if exiles != handBefore {
		t.Fatalf("exile events = %d, want %d (the WHOLE hand, not a single card)", exiles, handBefore)
	}
	draws := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw && ev.Player == 0 {
			draws++
		}
	}
	if draws != 3 {
		t.Fatalf("draw events = %d, want 3 (the paid body's Draw 3)", draws)
	}
	if got := libBefore - len(e.G.Zone(state.ZLibrary, 0)); got != 3 {
		t.Fatalf("library shrank by %d cards, want 3", got)
	}
	replayCheck(t, e, cfg)
}

// TestHerigastExileFromHandAllEmptyHandDeclines is the negative: the token
// still demands part.N (1) cards, so an empty hand cannot pay ExileFromHand
// <1/All> and the window is decline-only -- and declining moves nothing.
func TestHerigastExileFromHandAllEmptyHandDeclines(t *testing.T) {
	t.Parallel()
	// Herigast alone in hand: once it hits the stack the hand is empty.
	e, _, _ := herigastHandEngine(t)
	if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
		t.Fatalf("fixture hand = %d, want 1 (Herigast only)", got)
	}

	d := castHerigastAndOpenWindow(t, e)
	pay, decline := triggerCostWindowAskDecision(t, d)
	if pay >= 0 {
		t.Fatalf("an empty-hand ExileFromHand<1/All> cost was offered as payable: %+v", d.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, decline)
	passUntilStackEmpty(t, e, 40)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw || (ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZExile) {
			t.Fatalf("the unpayable cost still moved a card: %+v", ev)
		}
	}
}
