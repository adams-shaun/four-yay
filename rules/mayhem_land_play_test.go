package rules

// kw:Mayhem land shape: the bare, parameterless K:Mayhem (Oscorp Industries,
// the sole corpus carrier) is a permission to PLAY the card from your
// graveyard if you discarded it this turn -- not a cast, and no mana. The
// parameterized cast path is mayhem_test.go's; these tests pin the separate
// land-play offer (legal.go's mayhemLandPlayIds, gated by the same
// mayhemDiscardedThisTurn provenance and the ordinary land-drop/timing gates).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mayhemLandOptions returns seat p's play_land options for id.
func mayhemLandOptions(e *Engine, p state.PlayerID, id state.ObjID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(p) {
		if o.Kind == "play_land" && o.Obj == id {
			out = append(out, o)
		}
	}
	return out
}

// oscorpInGraveyard builds a two-card-hand engine holding Oscorp Industries,
// asserts the parsed land+Mayhem preconditions, and returns the engine and
// the card's id with the card still in hand.
func oscorpInHand(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusCard(t, "Oscorp Industries"))
	id := e.G.Zone(state.ZHand, 0)[0]
	f := e.G.Obj(id).Face()
	if !f.IsLand() {
		t.Fatalf("setup: Oscorp Industries parsed as %v, want a land", f.Types)
	}
	raw, ok := f.KeywordParam("Mayhem")
	if !ok || raw != "" {
		t.Fatalf("setup: Oscorp Mayhem param = %q (ok %v), want the bare empty parameter", raw, ok)
	}
	return e, id
}

// TestMayhemLandPlayOfferedAfterThisTurnDiscard pins the positive case: after
// the owner discards Oscorp Industries this turn it is offered exactly one
// play_land option, submitting it moves the card from the graveyard to the
// battlefield, and the play consumes the land drop.
func TestMayhemLandPlayOfferedAfterThisTurnDiscard(t *testing.T) {
	e, id := oscorpInHand(t)
	discardToGraveyard(t, e, id, 0)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone after discard = %v, want graveyard", got)
	}

	opts := mayhemLandOptions(e, 0, id)
	if len(opts) != 1 {
		t.Fatalf("this-turn discard offered %d bare-Mayhem play_land options, want 1: %+v",
			len(opts), e.legalActions(0))
	}
	if opts[0].Kind != "play_land" {
		t.Fatalf("bare Mayhem offer kind = %q, want play_land", opts[0].Kind)
	}
	if got := len(mayhemOptions(e, 0)); got != 0 {
		t.Fatalf("bare Mayhem offered %d cast options, want 0 (a land play is not a cast)", got)
	}

	submitChoices(t, e, opts[0].Index)
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("played land zone = %v, want battlefield", got)
	}
	if e.G.Players[0].LandsPlayed != 1 {
		t.Fatalf("LandsPlayed after the graveyard play = %d, want 1", e.G.Players[0].LandsPlayed)
	}
	// The drop is spent: no second bare-Mayhem play_land offer.
	if got := len(mayhemLandOptions(e, 0, id)); got != 0 {
		t.Fatalf("after the play, %d more offers, want 0", got)
	}
}

// TestMayhemLandPlayNotOfferedWithoutThisTurnDiscard pins the provenance
// gate's negative forms -- a non-discard graveyard entry, another player's
// discard and a previous turn's discard -- each followed by the positive
// case on the same card so the 0s are about provenance, not some other gate.
func TestMayhemLandPlayNotOfferedWithoutThisTurnDiscard(t *testing.T) {
	e, id := oscorpInHand(t)

	// 1. Mill-shaped move: a plain MoveZone carries no discard marker.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	e.priorityRound()
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone after mill = %v, want graveyard", got)
	}
	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("plain graveyard move offered bare-Mayhem play_land %d times, want 0", n)
	}

	// 2. Another player's discard this turn.
	backToHand(t, e, id)
	e.emit(events.Discard(id, 1))
	e.priorityRound()
	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("opponent's discard offered bare-Mayhem play_land %d times, want 0", n)
	}

	// 3. The owner's discard LAST turn (TurnChange bounds the window).
	backToHand(t, e, id)
	e.emit(events.Discard(id, 0))
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.priorityRound()
	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("last turn's discard offered bare-Mayhem play_land %d times, want 0", n)
	}

	// 4. The same card discarded THIS turn: the offer appears.
	backToHand(t, e, id)
	discardToGraveyard(t, e, id, 0)
	if n := len(mayhemLandOptions(e, 0, id)); n != 1 {
		t.Fatalf("this-turn discard offered bare-Mayhem play_land %d times, want 1", n)
	}
}

// TestMayhemLandPlayNotOfferedWhenGated pins that the ordinary land-play gates
// still bind: the offer vanishes when the land drop is spent or when it is not
// the controller's main phase with an empty stack, and returns when both hold.
func TestMayhemLandPlayNotOfferedWhenGated(t *testing.T) {
	e, id := oscorpInHand(t)
	discardToGraveyard(t, e, id, 0)

	// Precondition: the offer exists at sorcery speed with a drop remaining,
	// so the negatives below are about the gates, not a missing offer.
	if n := len(mayhemLandOptions(e, 0, id)); n != 1 {
		t.Fatalf("baseline offered %d, want 1", n)
	}

	e.G.Players[0].LandsPlayed = 1
	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("spent land drop offered bare-Mayhem play_land %d times, want 0", n)
	}
	e.G.Players[0].LandsPlayed = 0

	e.G.Step = state.StepUpkeep
	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("non-main phase offered bare-Mayhem play_land %d times, want 0", n)
	}
	e.G.Step = state.StepMain1
	if n := len(mayhemLandOptions(e, 0, id)); n != 1 {
		t.Fatalf("back at main1 offered %d, want 1", n)
	}
}

// TestMayhemParameterizedStillOffersCastNotLand pins the separation: a
// cost-bearing mayhem card discarded this turn keeps its graveyard CAST
// option and is never offered as a play_land.
func TestMayhemParameterizedStillOffersCastNotLand(t *testing.T) {
	e := handEngine(t, corpusCard(t, "Abomination, World Ravager"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if raw, ok := e.G.Obj(id).Face().KeywordParam("Mayhem"); !ok || raw == "" {
		t.Fatalf("setup: Abomination Mayhem param = %q (ok %v), want a priced parameter", raw, ok)
	}
	discardToGraveyard(t, e, id, 0)
	addMana(t, e, 0, "4444R")

	if n := len(mayhemLandOptions(e, 0, id)); n != 0 {
		t.Fatalf("parameterized mayhem offered %d play_land options, want 0", n)
	}
	if n := len(mayhemOptions(e, 0)); n != 1 {
		t.Fatalf("parameterized mayhem offered %d cast options, want 1", n)
	}
}
