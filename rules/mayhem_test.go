package rules

// kw:Mayhem (the Doom Prevails keyword): "You may cast this card from your
// graveyard for {4}{R} if you discarded it this turn. Timing rules still
// apply." Mayhem IS a cost substitution (the printed mayhem cost replaces the
// mana cost, the Miracle/Madness shape) gated on a discard-this-turn
// provenance, and carries NO post-resolution behaviour (no exile tail); the
// cast's one provenance record is the state.FlagMayhem pay-time CastInfo
// bit (the Card.CastSa Spell.Mayhem condition's read — see
// mayhem_castsa_test.go), so the pieces are the offer (legal.go's graveyard
// walk) and the charge (beginCast's "mayhem" mode), both through
// mayhemCastCost; the provenance gate is mayhemDiscardedThisTurn, log-derived like the
// warp-recast and foretell gates.
//
// The bare parameterless K:Mayhem (Oscorp Industries) is the separate
// "you may PLAY this card from your graveyard" land shape and is deliberately
// not offered here -- a land play is not a cast.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// mayhemOptions returns the mayhem cast options the seat currently has.
func mayhemOptions(e *Engine, p state.PlayerID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Mode == "mayhem" {
			out = append(out, o)
		}
	}
	return out
}

// discardToGraveyard emits the canonical discard move (the same event the
// engine's own discard effects produce, marker included) for id, then
// refreshes priority so legalActions sees the new state.
func discardToGraveyard(t *testing.T, e *Engine, id state.ObjID, p state.PlayerID) {
	t.Helper()
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("setup: card zone = %v, want hand before the discard", got)
	}
	e.emit(events.Discard(id, p))
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone after discard = %v, want graveyard", got)
	}
	e.priorityRound()
}

// backToHand moves a graveyard card back to its owner's hand by a real logged
// event (never a manual SetZone: a stale graveyard-zone entry would make the
// offer walk see the same object twice).
func backToHand(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard before the return", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZHand})
	e.priorityRound()
}

// TestAbominationWorldRavagerMayhemOfferedAndCasts pins the brief's
// end-to-end shape on the real corpus card: the card discarded this turn is
// offered a graveyard cast for the mayhem cost, the cast charges {4}{R} (not
// the printed {7}{R}), and the resolved spell enters the battlefield.
func TestAbominationWorldRavagerMayhemOfferedAndCasts(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Abomination, World Ravager"))
	id := e.G.Zone(state.ZHand, 0)[0]

	// Precondition 1: the parsed face carried the keyword and its parameter
	// through -- otherwise the offer below could never appear and every
	// later assertion would be vacuous.
	if !e.G.Obj(id).Face().HasKeyword("Mayhem") {
		t.Fatalf("setup: parsed face lost the Mayhem keyword: %v", e.G.Obj(id).Face().Keywords)
	}
	raw, ok := e.G.Obj(id).Face().KeywordParam("Mayhem")
	if !ok || raw != "4 R" {
		t.Fatalf("setup: Mayhem param = %q (ok %v), want \"4 R\"", raw, ok)
	}

	discardToGraveyard(t, e, id, 0)
	addMana(t, e, 0, "4444R")

	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != id {
		t.Fatalf("mayhem cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)

	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("mayhem spell zone = %v, want stack", got)
	}
	// No post-resolution behaviour: the cast carries only the CastSa
	// provenance bit, no entry-hook flag — a copy of this spell strips the
	// bit (state.CastProvenanceFlags), so it cannot inherit the provenance.
	if got := e.G.Obj(id).CastFlags; got != state.FlagMayhem {
		t.Fatalf("mayhem cast carries CastFlags %+v, want FlagMayhem only", got)
	}

	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Zone; got != state.ZBattlefield {
		t.Fatalf("resolved mayhem spell went to %v, want battlefield", got)
	}
}

// TestMayhemNotOfferedWithoutThisTurnDiscard pins the provenance gate in its
// negative forms on Chameleon, Master of Disguise ({2}{U} mayhem): a card
// that reached the graveyard by anything OTHER than a discard, or that was
// discarded on an EARLIER turn, gets no offer; the same card then discarded
// this turn DOES, proving the first assertions are about the provenance and
// not about some other gate.
func TestMayhemNotOfferedWithoutThisTurnDiscard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Chameleon, Master of Disguise"))
	id := e.G.Zone(state.ZHand, 0)[0]
	if _, ok := e.G.Obj(id).Face().KeywordParam("Mayhem"); !ok {
		t.Fatalf("setup: parsed face lost the Mayhem keyword: %v", e.G.Obj(id).Face().Keywords)
	}
	addMana(t, e, 0, "UUC")

	// 1. Mill-shaped move: a plain MoveZone carries no discard marker.
	if got := e.G.Obj(id).Zone; got != state.ZHand {
		t.Fatalf("setup: card zone = %v, want hand", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	e.priorityRound()
	if n := len(mayhemOptions(e, 0)); n != 0 {
		t.Fatalf("plain hand->graveyard move offered mayhem %d times, want 0", n)
	}
	// 2. Another player's discard this turn: still no offer for seat 0.
	backToHand(t, e, id)
	e.emit(events.Discard(id, 1))
	e.priorityRound()
	if n := len(mayhemOptions(e, 0)); n != 0 {
		t.Fatalf("opponent's discard offered mayhem %d times, want 0", n)
	}
	// 3. The owner's discard LAST turn (a TurnChange bounds the window).
	backToHand(t, e, id)
	e.emit(events.Discard(id, 0))
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.priorityRound()
	if n := len(mayhemOptions(e, 0)); n != 0 {
		t.Fatalf("last turn's discard offered mayhem %d times, want 0", n)
	}
	// 4. The same card discarded THIS turn: the offer appears.
	backToHand(t, e, id)
	discardToGraveyard(t, e, id, 0)
	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != id {
		t.Fatalf("this turn's discard offered %+v, want one mayhem cast", opts)
	}
}

// TestMayhemCostDiscardCounts pins the cost-discard half of the gate: a card
// discarded AS A COST (events.DiscardCost, which carries no Player field --
// the payer is the owner, CR 118.2a) opens the same window.
func TestMayhemCostDiscardCounts(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Chameleon, Master of Disguise"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.DiscardCost(id))
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone after cost discard = %v, want graveyard", got)
	}
	addMana(t, e, 0, "UUC")
	opts := mayhemOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != id {
		t.Fatalf("cost-discard provenance offered %+v, want one mayhem cast", opts)
	}
	submitChoices(t, e, opts[0].Index)
	if got := e.G.Obj(id).Zone; got != state.ZStack {
		t.Fatalf("mayhem spell zone = %v, want stack", got)
	}
}

// TestMayhemNotOfferedUnfundedOrMilled pins that the offer still respects the
// ordinary cast gates: an unfunded pool offers nothing, and so does a card
// whose graveyard entry was a mill move rather than a discard -- both on the
// same card, so the discriminator is the gate, not the card.
func TestMayhemNotOfferedUnfundedOrMilled(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Chameleon, Master of Disguise"))
	id := e.G.Zone(state.ZHand, 0)[0]
	discardToGraveyard(t, e, id, 0)
	if n := len(mayhemOptions(e, 0)); n != 0 {
		t.Fatalf("unfunded pool offered mayhem %d times, want 0", n)
	}
	// Prove the card itself can be offered when funded, so the 0 above was
	// about the empty pool.
	addMana(t, e, 0, "UUC")
	if n := len(mayhemOptions(e, 0)); n != 1 {
		t.Fatalf("funded mayhem options = %d, want 1: %+v", n, e.legalActions(0))
	}
}
