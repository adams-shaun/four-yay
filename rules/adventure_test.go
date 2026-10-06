package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// adventure_test.go pins the Adventure spell face (CR 714) end to end. The
// main-path test runs on the REAL corpus card (Brazen Borrower // Petty
// Theft); the counter and provenance edges run on inline fixtures (never
// Forge text, per the licensing rule). Every test ends in a replayCheck.

// adventureFixtureSrc is an authored Adventure card: an Instant Adventure
// spell face that destroys a creature, over a creature front.
const adventureFixtureSrc = "Name:Swindle\nManaCost:2 R\nTypes:Creature Goblin Rogue\nPT:3/1\n" +
	"AlternateMode:Adventure\nOracle:front\nALTERNATE\nName:Swipe\nManaCost:1 R\n" +
	"Types:Instant Adventure\nA:SP$ Destroy | ValidTgts$ Creature | SpellDescription$ Destroy target creature.\nOracle:back\n"

const adventureCancelSrc = "Name:Cancel\nManaCost:1 U U\nTypes:Instant\n" +
	"A:SP$ Counter | TargetType$ Spell | TgtPrompt$ Select target spell | ValidTgts$ Card\nOracle:x\n"

const adventureBearSrc = "Name:Fixture Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// adventureOption returns the cast option for id whose Mode is exactly mode,
// or nil when none is offered.
func adventureOption(t *testing.T, e *Engine, id state.ObjID, mode string) *decision.Option {
	t.Helper()
	opts := castOptions(t, e)
	for i := range opts {
		o := &opts[i]
		if o.Obj == id && o.Mode == mode {
			return o
		}
	}
	return nil
}

// adventureCorpusEngine deals seat 0 a 40-card deck led by Brazen Borrower
// and all Islands, and seat 1 a deck of Grizzly Bears, one of which is moved
// onto the battlefield as Petty Theft's target (ValidTgts$
// Permanent.nonLand+OppCtrl). The returned id is the Borrower in seat 0's
// hand. Corpus cards only -- no Forge script text is committed here.
func adventureCorpusEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	borrower := searchCorpusCard(t, reg, "Brazen Borrower")
	island := searchCorpusCard(t, reg, "Island")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{borrower}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = bear
	}
	cfg := seatZeroStart(Config{Seed: 7401, Names: []string{"adventure", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	oppBear := e.G.Zone(state.ZLibrary, 1)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: oppBear, From: state.ZLibrary, To: state.ZBattlefield})
	e.pending = nil
	id := searchMoveByName(t, e, "Brazen Borrower", state.ZHand)
	return e, cfg, id, oppBear
}

// TestAdventureSpellFaceOfferedCastAndRecast is the main-path pin, on the
// real corpus card: the hand offers BOTH faces; casting Petty Theft poses
// its target ask, resolves, and exiles the card into the adventure zone
// (ZExile as its main face, CR 715.4 -- NOT the graveyard); the engine then offers the
// main face from the adventure zone, casting it brings the creature in at
// its front face, and the adventure-zone offer is gone afterwards. The front
// face's ordinary cast from hand is present throughout.
func TestAdventureSpellFaceOfferedCastAndRecast(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, id, oppBear := adventureCorpusEngine(t, reg)
	addMana(t, e, 0, "UUU")
	plain := adventureOption(t, e, id, "")
	alt := adventureOption(t, e, id, "adventure_alt")
	if plain == nil || plain.Label != "Cast Brazen Borrower" {
		t.Fatalf("front-face cast option missing/renamed: %+v", castOptions(t, e))
	}
	if alt == nil || alt.Label != "Cast Petty Theft" {
		t.Fatalf("adventure_alt offer missing/renamed: %+v", castOptions(t, e))
	}

	// Cast the Adventure spell face; its target ask is posed.
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Petty Theft target ask: %+v", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == oppBear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option targeting the bear: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	// CR 715.4: in the adventure zone the card is its main face again.
	if o.Zone != state.ZExile || int(o.FaceIdx) != 0 {
		t.Fatalf("after Petty Theft resolved: zone=%s faceIdx=%d, want ZExile/0", o.Zone, o.FaceIdx)
	}
	if bounced := e.G.Obj(oppBear); bounced.Zone != state.ZHand {
		t.Fatalf("Petty Theft's target zone=%s, want hand", bounced.Zone)
	}

	// The main face is offered from the adventure zone.
	addMana(t, e, 0, "UU")
	recast := adventureOption(t, e, id, "adventure_recast")
	if recast == nil || recast.Label != "Cast Brazen Borrower (from adventure zone)" {
		t.Fatalf("adventure_recast offer missing/renamed: %+v", castOptions(t, e))
	}
	submitChoices(t, e, recast.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("creature recast posed a target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	o = e.G.Obj(id)
	if o.Zone != state.ZBattlefield || int(o.FaceIdx) != 0 {
		t.Fatalf("after the recast: zone=%s faceIdx=%d, want battlefield/0", o.Zone, o.FaceIdx)
	}

	// The adventure-zone offer is gone: the card left exile.
	addMana(t, e, 0, "U")
	if got := adventureOption(t, e, id, "adventure_recast"); got != nil {
		t.Fatalf("adventure_recast still offered after the card left exile: %+v", got)
	}
	replayCheck(t, e, cfg)
}

// TestAdventureSpellCounteredGoesToGraveyard pins the fizzle boundary: a
// countered Adventure spell NEVER resolved, so CR 714.3a's exile does not
// apply -- it goes to its owner's graveyard, and no adventure-zone recast is
// ever offered for it.
func TestAdventureSpellCounteredGoesToGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 7402, adventureFixtureSrc, adventureCancelSrc, adventureBearSrc)
	cancelID := addToHand(t, e, 0, adventureCancelSrc)
	bearID := putCreature(t, e, 0, adventureBearSrc)
	addMana(t, e, 0, "RRRUU")
	alt := adventureOption(t, e, id, "adventure_alt")
	if alt == nil {
		t.Fatalf("adventure_alt offer missing: %+v", castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)
	// Petty Theft's own target ask: the Swipe spell would destroy the bear --
	// but we counter it before it resolves.
	subMid := e.Pending()
	if subMid == nil || subMid.Kind != decision.KTarget {
		t.Fatalf("Swipe target ask: %+v", subMid)
	}
	tgt := -1
	for _, o := range subMid.Options {
		if o.Obj == bearID {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option targeting the bear: %+v", subMid.Options)
	}
	submitChoices(t, e, tgt)
	// The Swipe spell sits on the stack, paid for. Counter it before anyone
	// passes priority into resolution.
	addMana(t, e, 0, "UU")
	submitChoices(t, e, castOptionFor(t, e, cancelID).Index)
	subMid = e.Pending()
	if subMid == nil || subMid.Kind != decision.KTarget {
		t.Fatalf("no target decision for the counter: %+v", subMid)
	}
	tgt = -1
	for _, o := range subMid.Options {
		if o.Obj == id {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("no option targeting the Adventure spell: %+v", subMid.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.Zone != state.ZGraveyard {
		t.Fatalf("countered Adventure spell zone=%s, want ZGraveyard", o.Zone)
	}
	addMana(t, e, 0, "RR")
	if got := adventureOption(t, e, id, "adventure_recast"); got != nil {
		t.Fatalf("adventure_recast offered for a countered spell: %+v", got)
	}
	replayCheck(t, e, cfg)
}

// TestAdventureExileWithoutResolutionProvenance pins the scope boundary
// (CR 714.3b): only a RESOLVING adventure_alt cast puts a card in the
// adventure zone. A card exiled by another effect -- even while showing its
// Adventure spell face -- is plain exiled, and the recast is never offered.
func TestAdventureExileWithoutResolutionProvenance(t *testing.T) {
	t.Parallel()
	e, cfg, id := newFixtureDeck(t, 7403, adventureFixtureSrc)
	// Route 1: exiled from the hand, still showing its front face.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "RRR")
	if got := adventureOption(t, e, id, "adventure_recast"); got != nil {
		t.Fatalf("adventure_recast offered for a front-face exile: %+v", got)
	}
	// Route 2: the same card flipped to its spell face and exiled again --
	// the face alone is not provenance; the log is.
	e.emit(events.Event{Kind: events.FlipFace, Obj: id, Amount: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZExile, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZExile})
	e.pending = nil
	e.priorityRound()
	addMana(t, e, 0, "RRR")
	if got := adventureOption(t, e, id, "adventure_recast"); got != nil {
		t.Fatalf("adventure_recast offered for a spell-face exile with no Adventure cast in the log: %+v", got)
	}
	replayCheck(t, e, cfg)
}
