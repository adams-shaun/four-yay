package rules

// kw:Jump-start (CR 702.84a): "You may cast this card from your graveyard by
// discarding a card in addition to paying its other costs. Then exile this
// card." Jump-start is NOT a cost substitution (the oracle reads "in addition
// to paying its other costs"), exactly the Retrace shape -- printed mana cost
// PLUS an additional discard -- but the discard may be any card (not just a
// land) and the spell is exiled as it resolves, like flashback. The offer
// lives in legal.go's graveyard walk and the charge in beginCast's
// "jumpstart" mode, both through jumpstartExtra so they cannot drift; the
// exile destination is modeFlags' FlagJumpstart, read by
// spellRestZone/spellFizzleZone.
//
// Radical Idea is the Quick Draw census carrier named by the brief: a {1}{U}
// instant whose only effect is "draw a card", so before this work it could
// never be cast a second time from the graveyard.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// jumpstartOptions returns the jump-start cast options the seat currently has.
func jumpstartOptions(e *Engine, p state.PlayerID) []decision.Option {
	var out []decision.Option
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Mode == "jumpstart" {
			out = append(out, o)
		}
	}
	return out
}

// TestRadicalIdeaJumpstartCastsDiscardsAndExiles pins the brief's end-to-end
// shape on the real corpus card: with any card in hand and the printed {1}{U}
// payable, the graveyard cast is offered, the discard ask offers exactly the
// hand card, and answering it moves that card from hand to graveyard and puts
// the spell on the stack; after resolution the spell is exiled and its
// controller has drawn a card.
func TestRadicalIdeaJumpstartCastsDiscardsAndExiles(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Radical Idea"))
	cardID := graveyardCorpus(t, e)

	// Precondition 1: the jump-start card really is in the graveyard and its
	// keyword made it through the parser -- otherwise the offer below could
	// never appear and every later assertion would be vacuous.
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	if !e.G.Obj(cardID).Face().HasKeyword("Jump-start") {
		t.Fatalf("setup: parsed face lost the Jump-start keyword: %v", e.G.Obj(cardID).Face().Keywords)
	}
	// Precondition 2: the deck really has cards to draw, so "drew a card" can
	// fail loudly rather than pass on an empty library.
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("setup: library is empty, the draw assertion could not fail")
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))

	// Any card may pay the discard -- a nonland instant is the discriminator
	// against Retrace's discard-a-land gate.
	ditch := e.G.AddObject(card(t, "Name:Test Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"), 0)
	ditch.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ditch.ID})
	if got := e.G.Obj(ditch.ID).Zone; got != state.ZHand {
		t.Fatalf("setup: discard-fodder zone = %v, want hand", got)
	}

	addMana(t, e, 0, "UU1")

	opts := jumpstartOptions(e, 0)
	if len(opts) != 1 || opts[0].Obj != cardID {
		t.Fatalf("jump-start cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)

	// The additional cost ask: exactly the one hand card, Min=Max=1.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("discard cost ask missing: %+v", d)
	}
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 1 {
		t.Fatalf("discard ask shape = min %d max %d opts %d, want 1/1/1", d.Min, d.Max, len(d.Options))
	}
	if d.Options[0].Obj != ditch.ID {
		t.Fatalf("discard option = %+v, want the hand card %d", d.Options[0], ditch.ID)
	}
	submitChoices(t, e, d.Options[0].Index)

	if got := e.G.Obj(ditch.ID).Zone; got != state.ZGraveyard {
		t.Fatalf("discarded card zone = %v, want graveyard (discard was not paid)", got)
	}
	if got := e.G.Obj(cardID).Zone; got != state.ZStack {
		t.Fatalf("jump-start spell zone = %v, want stack", got)
	}
	// Precondition 3: the provenance flag the exile reader depends on is set.
	if e.G.Obj(cardID).CastFlags&state.FlagJumpstart == 0 {
		t.Fatalf("jump-start cast carries no FlagJumpstart: %+v", e.G.Obj(cardID).CastFlags)
	}

	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(cardID).Zone; got != state.ZExile {
		t.Fatalf("resolved jump-start spell went to %v, want exile", got)
	}
	// Radical Idea's SP$ Draw resolved: the controller's hand is back to its
	// size after discarding the fodder (net zero) plus the drawn card.
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("hand size after resolution = %d, want %d (Radical Idea did not draw)", got, handBefore+1)
	}
}

// TestRadicalIdeaJumpstartNotOfferedWithoutACardToDiscard pins the negative
// gate: with the printed cost payable but an empty hand there is nothing to
// discard, so no jump-start option may be offered. A card then joins the hand
// and the offer appears, proving the first absence was the discard gate.
func TestRadicalIdeaJumpstartNotOfferedWithoutACardToDiscard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Radical Idea"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	// handEngine leaves no other hand card behind.
	e.G.SetZone(state.ZHand, 0, nil)

	addMana(t, e, 0, "UU1")
	if got := jumpstartOptions(e, 0); len(got) != 0 {
		t.Fatalf("jump-start offered with an empty hand: %+v", got)
	}

	ditch := e.G.AddObject(card(t, "Name:Test Forest\nTypes:Basic Land Forest\nOracle:x\n"), 0)
	ditch.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ditch.ID})
	if got := jumpstartOptions(e, 0); len(got) != 1 || got[0].Obj != cardID {
		t.Fatalf("jump-start still not offered once a card is in hand: %+v", e.legalActions(0))
	}
}

// TestRadicalIdeaJumpstartNotOfferedWhenManaUnpayable pins the other half of
// the offer gate: a card is in hand but the printed {1}{U} is not payable, so
// the cast must be withheld.
func TestRadicalIdeaJumpstartNotOfferedWhenManaUnpayable(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Radical Idea"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	ditch := e.G.AddObject(card(t, "Name:Test Island\nTypes:Basic Land Island\nOracle:x\n"), 0)
	ditch.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ditch.ID})

	// Only one colourless in the pool, short of {1}{U}.
	addMana(t, e, 0, "1")
	if got := jumpstartOptions(e, 0); len(got) != 0 {
		t.Fatalf("jump-start offered without payable mana: %+v", got)
	}
}

// TestJumpstartOfferAbsentOnAnOrdinaryGraveyardCard is the control: a card in
// the graveyard with no Jump-start keyword gets no jumpstart option, so the
// offers above are attributable to the keyword rather than to the graveyard
// walk.
func TestJumpstartOfferAbsentOnAnOrdinaryGraveyardCard(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Radical Idea"))
	plain := graveyardCard(t, e, "Name:Plain Spell\nManaCost:1 U\nTypes:Sorcery\nOracle:x\n")
	if got := e.G.Obj(plain).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	ditch := e.G.AddObject(card(t, "Name:Test Swamp\nTypes:Basic Land Swamp\nOracle:x\n"), 0)
	ditch.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ditch.ID})
	addMana(t, e, 0, "UU1")
	if got := jumpstartOptions(e, 0); len(got) != 0 {
		t.Fatalf("ordinary graveyard card offered a jump-start cast: %+v", got)
	}
}

// TestJumpstartedSpellCounteredGoesToExile: CR 702.84a's "then exile this
// card" is the flashback convention -- "any time it would leave the stack",
// which includes being countered. effCounter must not send it back to the
// graveyard where it could be jump-started again.
func TestJumpstartedSpellCounteredGoesToExile(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusCard(t, "Radical Idea"))
	cardID := graveyardCorpus(t, e)
	if got := e.G.Obj(cardID).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	ditch := e.G.AddObject(card(t, "Name:Test Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"), 0)
	ditch.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{ditch.ID})
	addMana(t, e, 0, "UU1")

	opts := jumpstartOptions(e, 0)
	if len(opts) != 1 {
		t.Fatalf("jump-start cast not offered: %+v", e.legalActions(0))
	}
	submitChoices(t, e, opts[0].Index)
	submitChoices(t, e, 0) // discard the fodder
	if e.G.Obj(cardID).Zone != state.ZStack {
		t.Fatalf("spell %s, want stack", e.G.Obj(cardID).Zone)
	}
	// Counter it with a hand-built Counter effect against the stack object --
	// the same shape TestFlashbackedSpellCounteredGoesToExile uses.
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: cardID}}, TargetsOffered: true},
		card(t, "Name:Counterspell\nManaCost:U U\nTypes:Instant\nA:SP$ Counter | ValidTgts$ Spell\nOracle:x\n").Faces[0].SpellAbility())
	if got := e.G.Obj(cardID).Zone; got != state.ZExile {
		t.Fatalf("countered jump-started spell went to %v, want exile", got)
	}
}
