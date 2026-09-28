package rules

// kw:Retrace (CR 702.81a): "You may cast this card from your graveyard by
// discarding a land card in addition to paying its other costs." Unlike the
// alternative-cost keyword family (Escape, Flashback, Madness) Retrace is NOT
// a cost substitution: the printed mana cost is still paid, and the discard is
// a plain ADDITIONAL cost folded on top. The offer therefore has two gates
// beyond timing and targets -- the printed cost must be payable AND a land
// card must actually be in hand to discard (an option that cannot be paid must
// never be offered). Both the offer (legal.go's graveyard walk) and the charge
// (beginCast's "retrace" mode) build the additional part through retraceExtra,
// so they cannot drift.
//
// Embrace the Unknown is the corpus carrier named by the brief: an OTC precon
// card whose SP$ Dig -> DB$ Effect MayPlay body is otherwise implemented, and
// which before this work could not be cast at all because its only zone after
// the first cast is the graveyard.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// retraceOptions returns the retrace cast options the seat currently has.
func retraceOptions(e *Engine, p state.PlayerID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Mode == "retrace" {
			out = append(out, o)
		}
	}
	return out
}

// graveyardCorpus moves the single card handEngine seated in seat 0's hand
// into its graveyard, where Retrace casts are offered from.
func graveyardCorpus(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	hand := e.G.Zone(state.ZHand, 0)
	if len(hand) != 1 {
		t.Fatalf("setup: want exactly one hand card, got %d", len(hand))
	}
	id := hand[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	return id
}

// TestEmbraceTheUnknownRetraceOfferedAndDiscardsALand pins the brief's
// end-to-end shape on the real corpus card: with a land in hand and the
// printed {2}{R} payable, the graveyard cast is offered, the discard ask
// offers exactly the land, and answering it moves that land from hand to
// graveyard and puts the spell on the stack.
func TestEmbraceTheUnknownRetraceOfferedAndDiscardsALand(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Embrace the Unknown"))
	cardID := graveyardCorpus(t, e)

	// Precondition: the retrace card really is in the graveyard, and its
	// keyword made it through the parser (otherwise the offer below could
	// never appear and the negative assertions would be vacuous).
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	if !e.G.Obj(cardID).Face().HasKeyword("Retrace") {
		t.Fatalf("setup: parsed face lost the Retrace keyword: %v", e.G.Obj(cardID).Face().Keywords)
	}

	land := e.G.AddObject(card(t, "Name:Test Mountain\nTypes:Basic Land Mountain\nOracle:x\n"), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{land.ID})
	if got := e.G.Obj(land.ID).Zone; got != state.ZHand {
		t.Fatalf("setup: land zone = %v, want hand", got)
	}

	addMana(t, e, 0, "RR2")

	opts := retraceOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != cardID {
		t.Fatalf("retrace cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)

	// The additional cost ask: exactly the one land, Min=Max=1.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("discard cost ask missing: %+v", d)
	}
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 1 {
		t.Fatalf("discard ask shape = min %d max %d opts %d, want 1/1/1", d.Min, d.Max, len(d.Options))
	}
	if d.Options[0].Obj != land.ID {
		t.Fatalf("discard option = %+v, want the land %d", d.Options[0], land.ID)
	}
	submitChoices(t, e, d.Options[0].Index)

	if got := e.G.Obj(land.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("discarded land zone = %v, want graveyard (discard was not paid)", got)
	}
	if got := e.G.Obj(cardID).Zone; got != state.ZStack {
		t.Fatalf("retrace spell zone = %v, want stack", got)
	}
}

// TestEmbraceTheUnknownRetraceNotOfferedWithoutALandToDiscard pins the
// negative gate: the printed cost is payable, but with no land in hand the
// additional cost cannot be paid, so no retrace option may be offered. The
// same card is then given a land and the offer appears, proving the first
// assertion is about the cost and not about some other gate.
func TestEmbraceTheUnknownRetraceNotOfferedWithoutALandToDiscard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Embrace the Unknown"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}

	// A nonland in hand: not discardable for this cost.
	spell := e.G.AddObject(card(t, "Name:Test Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"), 0)
	spell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{spell.ID})

	addMana(t, e, 0, "RR2")
	if got := retraceOptions(e, 0); len(got) != 0 {
		t.Fatalf("retrace offered with no land to discard: %+v", got)
	}

	// Now a land joins the hand: the offer must appear, so the earlier
	// absence was the land gate and nothing else.
	land := e.G.AddObject(card(t, "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{spell.ID, land.ID})
	if got := retraceOptions(e, 0); len(got) != 1 || got[0].Obj != cardID {
		t.Fatalf("retrace still not offered once a land is in hand: %+v", e.legalActions(0))
	}
}

// TestRetraceNotOfferedWhenManaUnpayable pins the other half of the offer
// gate: a land is in hand, but the printed {2}{R} is not payable, so the cast
// must be withheld.
func TestRetraceNotOfferedWhenManaUnpayable(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Embrace the Unknown"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	land := e.G.AddObject(card(t, "Name:Test Island\nTypes:Basic Land Island\nOracle:x\n"), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{land.ID})

	// Only one generic in the pool, short of {2}{R}.
	addMana(t, e, 0, "1")
	if got := retraceOptions(e, 0); len(got) != 0 {
		t.Fatalf("retrace offered without payable mana: %+v", got)
	}
}

// TestRetraceOfferAbsentOnAnOrdinaryGraveyardCard is the control: a card in
// the graveyard with no Retrace keyword gets no retrace option, so the offers
// above are attributable to the keyword rather than to the graveyard walk.
func TestRetraceOfferAbsentOnAnOrdinaryGraveyardCard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Embrace the Unknown"))
	plain := graveyardCard(t, e, "Name:Plain Spell\nManaCost:R\nTypes:Sorcery\nOracle:x\n")
	if got := e.G.Obj(plain).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	land := e.G.AddObject(card(t, "Name:Test Swamp\nTypes:Basic Land Swamp\nOracle:x\n"), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{land.ID})
	addMana(t, e, 0, "RR2")
	if got := retraceOptions(e, 0); len(got) != 0 {
		t.Fatalf("ordinary graveyard card offered a retrace cast: %+v", got)
	}
}
