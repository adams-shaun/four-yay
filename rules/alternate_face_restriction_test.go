package rules

// alternate_face_restriction_test.go pins the CantBeCast face probe on every
// alternate-face cast route the brief names: the hand split_alt/adventure_alt
// offers, the graveyard aftermath offer and the exile adventure_recast offer.
//
// Before this change each route gated on the card-level castRestricted, which
// reads the DISPLAYED face, while offering/pricing an alternate face:
//
//   - split_alt and adventure_alt sat behind `if castRestricted(p, id) {
//     continue }`, so a restriction matching only the front face silently
//     withheld an otherwise-legal alternate-face offer (direction 1);
//   - aftermath and adventure_recast gated on the displayed face too, so a
//     restriction matching only the face the cast flips to was missed and the
//     prohibited cast was offered, then reversed by the CR 601.2e recheck and
//     re-offered (direction 2).
//
// The fix probes each route with castRestrictedAsFace against the face the
// cast actually uses (the same probe rules/faceprobe.go already prices with
// and rules/cast.go's beginCast flips to). Corpus cards only -- no Forge
// script text is committed here.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// altFaceOption returns the cast option for id whose Mode is exactly mode, or
// nil when none is offered. It reads e.legalActions directly rather than the
// pending snapshot, so it can be called at any point in a transaction.
func altFaceOption(e *Engine, id state.ObjID, mode string) *decision.Option {
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			cp := o
			return &cp
		}
	}
	return nil
}

// probeFaceRestricted evaluates castRestricted against face under the same
// offerAsFace probe the offer walk prices with -- the precondition every test
// here asserts before it trusts its direction.
func probeFaceRestricted(t *testing.T, e *Engine, id state.ObjID, face *cards.Face) bool {
	t.Helper()
	if face == nil {
		t.Fatal("precondition: probe face is nil")
	}
	return e.offerAsFace(id, face, func() bool {
		return e.castRestricted(0, id)
	})
}

// moveRestrictionSource event-sources a corpus restriction carrier from seat
// 0's hand/library onto the battlefield. Unlike addCorpusBattlefield this is
// replay-safe (the card is a real deck object, so a log replay rebuilds it),
// which the split/aftermath tests' replayCheck needs. Search-restriction
// sources must be part of the engine's deck.
func moveRestrictionSource(t *testing.T, e *Engine, name string) state.ObjID {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZBattlefield)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: %s is zone=%v, want battlefield", name, o)
	}
	return id
}

// resolvePettyTheftFromHand casts Brazen Borrower's Adventure face (Petty
// Theft) from seat 0's hand onto the opponent's bear and resolves it, leaving
// the card in the adventure zone (ZExile, face 1) -- the only entry the
// adventure_recast offer's provenance check accepts. It returns nothing; the
// caller reads the card by id afterwards.
func resolvePettyTheftFromHand(t *testing.T, e *Engine, id, oppBear state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "UUU")
	alt := adventureOption(t, e, id, "adventure_alt")
	if alt == nil {
		t.Fatalf("precondition: adventure_alt offer missing before the resolve: %+v", castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: Petty Theft posed %+v, want a target ask", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Obj == oppBear {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: no option targeting the bear: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.Zone != state.ZExile || int(o.FaceIdx) != 1 {
		t.Fatalf("precondition: card after Petty Theft is zone=%s faceIdx=%d, want ZExile/1",
			o.Zone, o.FaceIdx)
	}
}

// --- Adventure face from hand (adventure_alt) -------------------------------

// TestAlternateFaceCantBeCastAdventureAltFrontRestricted is the "front face
// prohibited" direction on the hand Adventure offer: Steel Golem's
// ValidCard$ Creature CantBeCast matches the Brazen Borrower creature front
// but not the Petty Theft Instant back, so the adventure_alt offer must
// survive (the old card-level gate withheld it) and casting it must select
// the Petty Theft face.
func TestAlternateFaceCantBeCastAdventureAltFrontRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg, id, oppBear := adventureCorpusEngine(t, reg)
	golem := addCorpusBattlefield(t, e, "Steel Golem")
	goObj := e.G.Obj(golem)
	if goObj.Zone != state.ZBattlefield || !goObj.Face().IsCreature() {
		t.Fatalf("precondition: Steel Golem zone=%s face=%q, want a creature on the battlefield",
			goObj.Zone, goObj.Face().Name)
	}
	// Precondition: the FRONT face is restricted -- this is exactly the gate
	// that withheld the adventure_alt offer before the fix.
	if !e.castRestricted(0, id) {
		t.Fatal("precondition: the Brazen Borrower front is not restricted by " +
			"Steel Golem's ValidCard$ Creature static")
	}
	// Precondition: the ADVENTURE face is NOT restricted under the same probe.
	if probeFaceRestricted(t, e, id, adventureSpellFace(e.G.Obj(id))) {
		t.Fatal("precondition: the Petty Theft face is restricted, so this is " +
			"not the front-only direction")
	}

	addMana(t, e, 0, "UU")
	alt := adventureOption(t, e, id, "adventure_alt")
	if alt == nil {
		t.Fatalf("adventure_alt withheld although only the creature front is "+
			"restricted: %+v", castOptions(t, e))
	}
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
	if o.Zone != state.ZExile || int(o.FaceIdx) != 1 || o.Face().Name != "Petty Theft" {
		t.Fatalf("adventure_alt did not resolve as Petty Theft: zone=%s faceIdx=%d name=%q",
			o.Zone, o.FaceIdx, o.Face().Name)
	}
	if bounced := e.G.Obj(oppBear); bounced.Zone != state.ZHand {
		t.Fatalf("Petty Theft's target zone=%s, want hand", bounced.Zone)
	}
	_ = cfg
}

// TestAlternateFaceCantBeCastAdventureAltBackRestricted is the "back face
// prohibited" direction: Nikya of the Old Ways' Card.nonCreature CantBeCast
// matches the Petty Theft Instant back but not the Brazen Borrower creature
// front, so the adventure_alt offer must be withheld even though the front is
// unrestricted -- while the ordinary front cast stays offered.
func TestAlternateFaceCantBeCastAdventureAltBackRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _, id, _ := adventureCorpusEngine(t, reg)
	nikya := addCorpusBattlefield(t, e, "Nikya of the Old Ways")
	no := e.G.Obj(nikya)
	if no.Zone != state.ZBattlefield || no.Face().Name != "Nikya of the Old Ways" {
		t.Fatalf("precondition: Nikya zone=%s name=%q, want on the battlefield",
			no.Zone, no.Face().Name)
	}
	// Precondition: the front face is NOT restricted by the noncreature
	// static -- otherwise the card-level gate alone would withhold the offer
	// and the fixture would be vacuous.
	if e.castRestricted(0, id) {
		t.Fatal("precondition: the Brazen Borrower creature front is restricted, " +
			"so the back-face-only fixture is vacuous")
	}
	// Precondition: the ADVENTURE face really is restricted under the probe.
	if !probeFaceRestricted(t, e, id, adventureSpellFace(e.G.Obj(id))) {
		t.Fatal("precondition: the Petty Theft face is not restricted by Nikya's " +
			"Card.nonCreature static under the face probe")
	}

	addMana(t, e, 0, "UUUU")
	if alt := adventureOption(t, e, id, "adventure_alt"); alt != nil {
		t.Fatalf("adventure_alt offered for a face the CantBeCast static "+
			"prohibits: %+v", alt)
	}
	if plain := adventureOption(t, e, id, ""); plain == nil || plain.Label != "Cast Brazen Borrower" {
		t.Fatalf("front creature cast withheld although it is unrestricted: %+v",
			castOptions(t, e))
	}
}

// --- Adventure face from exile (adventure_recast) ---------------------------

// TestAlternateFaceCantBeCastAdventureRecastFrontRestricted: the exiled card
// still displays its Adventure face (Petty Theft, an Instant, which Nikya's
// Card.nonCreature static restricts), but the recast casts the creature front
// (Brazen Borrower), which is unrestricted. The recast must be offered and
// must resolve at the creature face (the old !castRestricted gate read the
// displayed Adventure face and withheld it).
func TestAlternateFaceCantBeCastAdventureRecastFrontRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg, id, oppBear := adventureCorpusEngine(t, reg)
	resolvePettyTheftFromHand(t, e, id, oppBear)

	nikya := addCorpusBattlefield(t, e, "Nikya of the Old Ways")
	if n := e.G.Obj(nikya); n.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Nikya zone=%s, want battlefield", n.Zone)
	}
	// Precondition: the DISPLAYED face (Petty Theft) IS restricted by the
	// noncreature static -- the gate the old recast path read.
	if !e.castRestricted(0, id) {
		t.Fatal("precondition: the displayed Petty Theft face is not restricted, " +
			"so the displayed-face probe would not withhold the recast")
	}
	// Precondition: the MAIN face the recast actually casts is NOT restricted.
	front := e.G.Obj(id).Card.Faces[0]
	if probeFaceRestricted(t, e, id, front) {
		t.Fatal("precondition: the Brazen Borrower main face is restricted, so " +
			"the recast should legitimately be withheld")
	}
	if front.Name != "Brazen Borrower" {
		t.Fatalf("precondition: main face is %q, want Brazen Borrower", front.Name)
	}

	addMana(t, e, 0, "UUU")
	recast := adventureOption(t, e, id, "adventure_recast")
	if recast == nil {
		t.Fatalf("adventure_recast withheld although only the displayed "+
			"Adventure face is restricted: %+v", castOptions(t, e))
	}
	submitChoices(t, e, recast.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("creature recast posed a target ask: %+v", d)
	}
	passUntilStackEmpty(t, e, 20)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || int(o.FaceIdx) != 0 || o.Face().Name != "Brazen Borrower" {
		t.Fatalf("adventure_recast did not enter as the creature: zone=%s faceIdx=%d name=%q",
			o.Zone, o.FaceIdx, o.Face().Name)
	}
	_ = cfg
}

// TestAlternateFaceCantBeCastAdventureRecastBackRestricted: the exiled card
// displays Petty Theft (an Instant, unrestricted by Steel Golem's
// ValidCard$ Creature static), but the recast casts the Brazen Borrower
// creature, which Steel Golem prohibits. The recast must be withheld (the old
// !castRestricted gate read the unrestricted displayed face and offered the
// prohibited creature cast).
func TestAlternateFaceCantBeCastAdventureRecastBackRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg, id, oppBear := adventureCorpusEngine(t, reg)
	resolvePettyTheftFromHand(t, e, id, oppBear)

	golem := addCorpusBattlefield(t, e, "Steel Golem")
	if g := e.G.Obj(golem); g.Zone != state.ZBattlefield || !g.Face().IsCreature() {
		t.Fatalf("precondition: Steel Golem zone=%s face=%q, want a creature on the battlefield",
			g.Zone, g.Face().Name)
	}
	// Precondition: the DISPLAYED Petty Theft face is NOT restricted -- this
	// is the gate the old recast path read and passed.
	if e.castRestricted(0, id) {
		t.Fatal("precondition: the displayed Petty Theft face is restricted, so " +
			"the displayed-face probe would already withhold the recast")
	}
	// Precondition: the MAIN face the recast casts IS restricted under the
	// probe -- the prohibition the old path missed.
	front := e.G.Obj(id).Card.Faces[0]
	if !probeFaceRestricted(t, e, id, front) {
		t.Fatal("precondition: the Brazen Borrower main face is not restricted by " +
			"Steel Golem's ValidCard$ Creature static")
	}

	addMana(t, e, 0, "UUU")
	if recast := adventureOption(t, e, id, "adventure_recast"); recast != nil {
		t.Fatalf("adventure_recast offered for a creature face the CantBeCast "+
			"static prohibits: %+v", recast)
	}
	_ = cfg
}

// --- Split alternate half from hand (split_alt) -----------------------------

// TestAlternateFaceCantBeCastSplitAltFrontRestricted: Gaddock Teeg's
// Card.nonCreature+cmcGE4 CantBeCast matches Order (mana value 4) but not
// Chaos (mana value 3), so Order // Chaos's split_alt offer for Chaos must
// survive the prohibited front half (the old card-level continue withheld
// it), and casting it must select the Chaos face.
func TestAlternateFaceCantBeCastSplitAltFrontRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Order", "Gaddock Teeg")
	id := searchMoveByName(t, e, "Order", state.ZHand)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	o := e.G.Obj(id)
	if o.Zone != state.ZHand || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Order zone=%s faceIdx=%d, want hand/front", o.Zone, o.FaceIdx)
	}
	sf := splitAlternateCastFace(o)
	if sf == nil || sf.Name != "Chaos" {
		t.Fatalf("precondition: split alternate face is %v, want Chaos", sf)
	}
	// Precondition: the FRONT half (Order, mv 4) is restricted; the ALTERNATE
	// half (Chaos, mv 3) is not. If they agreed, neither direction could be
	// observed.
	if !e.castRestricted(0, id) {
		t.Fatal("precondition: Order (mv 4) is not restricted by Gaddock Teeg's " +
			"Card.nonCreature+cmcGE4 static")
	}
	if probeFaceRestricted(t, e, id, sf) {
		t.Fatal("precondition: Chaos (mv 3) is restricted too, so the fixture is vacuous")
	}

	addMana(t, e, 0, "RRR")
	alt := altFaceOption(e, id, "split_alt")
	if alt == nil || alt.Label != "Cast Chaos" {
		t.Fatalf("split_alt withheld although only the front half is restricted: %+v",
			castOptions(t, e))
	}
	submitChoices(t, e, alt.Index)
	so := e.G.Obj(id)
	if so.Zone != state.ZStack || int(so.FaceIdx) != 1 || so.Face().Name != "Chaos" {
		t.Fatalf("split_alt did not select Chaos: zone=%s faceIdx=%d name=%q",
			so.Zone, so.FaceIdx, so.Face().Name)
	}
	passUntilStackEmpty(t, e, 20)
	replayCheck(t, e, cfg)
}

// TestAlternateFaceCantBeCastSplitAltBackRestricted: Gaddock Teeg's
// Card.nonCreature+cmcGE4 matches Call (mana value 6) but not Beck (mana
// value 2), so Beck // Call's split_alt offer for the prohibited Call must be
// withheld while Beck's ordinary front cast stays offered (the old card-level
// gate read the unrestricted front and offered Call).
func TestAlternateFaceCantBeCastSplitAltBackRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Beck", "Gaddock Teeg")
	id := searchMoveByName(t, e, "Beck", state.ZHand)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	o := e.G.Obj(id)
	if o.Zone != state.ZHand || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Beck zone=%s faceIdx=%d, want hand/front", o.Zone, o.FaceIdx)
	}
	sf := splitAlternateCastFace(o)
	if sf == nil || sf.Name != "Call" {
		t.Fatalf("precondition: split alternate face is %v, want Call", sf)
	}
	// Precondition: the FRONT half (Beck, mv 2) is unrestricted; the
	// ALTERNATE half (Call, mv 6) IS restricted.
	if e.castRestricted(0, id) {
		t.Fatal("precondition: Beck (mv 2) is restricted, so the front half is " +
			"not the unrestricted control this direction needs")
	}
	if !probeFaceRestricted(t, e, id, sf) {
		t.Fatal("precondition: Call (mv 6) is not restricted by Gaddock Teeg's " +
			"Card.nonCreature+cmcGE4 static")
	}

	// Fund the ALTERNATE half's own cost ({4}{W}{U}) as well as the front's,
	// so the only gate that can withhold split_alt is the restriction -- an
	// unaffordable half would pass the test for the wrong reason.
	addMana(t, e, 0, "WWUUGG")
	alt := altFaceOption(e, id, "split_alt")
	if alt != nil {
		t.Fatalf("split_alt offered for a half the CantBeCast static prohibits: %+v", alt)
	}
	if plain := altFaceOption(e, id, ""); plain == nil || plain.Label != "Cast Beck" {
		t.Fatalf("Beck's ordinary front cast withheld although it is unrestricted: %+v",
			castOptions(t, e))
	}
	replayCheck(t, e, cfg)
}

// --- Aftermath half from the graveyard (aftermath) --------------------------

// leaveSpellOnStackAtOpponent casts Lightning Bolt (already moved to seat 0's
// hand) at seat 1 and returns with the stack still holding it -- the legal
// target Cooperate's ValidTgts$ Instant,Sorcery needs before its aftermath
// offer can exist at all. It asserts the Bolt really is on the stack and that
// seat 0 holds priority afterwards.
func leaveSpellOnStackAtOpponent(t *testing.T, e *Engine, bolt state.ObjID) {
	t.Helper()
	opt := altFaceOption(e, bolt, "")
	if opt == nil {
		t.Fatalf("precondition: Lightning Bolt not castable: %+v", castOptions(t, e))
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("precondition: Bolt posed %+v, want a target ask", d)
	}
	tgt := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tgt = o.Index
		}
	}
	if tgt < 0 {
		t.Fatalf("precondition: Bolt offered no option for opponent player 1: %+v", d.Options)
	}
	submitChoices(t, e, tgt)
	// The engine hands priority to the opponent after a cast; pass once so
	// seat 0 holds it again with the Bolt still on the stack.
	if pd := e.Pending(); pd != nil && pd.Kind == decision.KPriority && pd.Player != 0 {
		submitPass(t, e)
	}
	if z := e.G.Obj(bolt).Zone; z != state.ZStack {
		t.Fatalf("precondition: Bolt zone=%s, want on the stack", z)
	}
}

// TestAlternateFaceCantBeCastAftermathFrontRestricted: Gaddock Teeg's
// Card.nonCreature+cmcGE4 matches Refuse (mana value 4) but not Cooperate
// (mana value 3, the K:Aftermath half), so Refuse // Cooperate resting in the
// graveyard must still offer the aftermath cast of Cooperate (the old
// card-level gate read the restricted front and withheld it), and casting it
// must select the Cooperate face. A Lightning Bolt sits on the stack
// throughout: Cooperate's only legal target is an instant/sorcery spell, and
// without one its offer is withheld for a reason unrelated to the
// restriction, which would make the test vacuous.
func TestAlternateFaceCantBeCastAftermathFrontRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Refuse", "Gaddock Teeg", "Lightning Bolt")
	id := searchMoveByName(t, e, "Refuse", state.ZGraveyard)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	bolt := searchMoveByName(t, e, "Lightning Bolt", state.ZHand)
	o := e.G.Obj(id)
	if o.Zone != state.ZGraveyard || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Refuse zone=%s faceIdx=%d, want graveyard/front",
			o.Zone, o.FaceIdx)
	}
	af := aftermathAlternateFace(o)
	if af == nil || af.Name != "Cooperate" {
		t.Fatalf("precondition: aftermath alternate face is %v, want Cooperate", af)
	}
	// Precondition: the FRONT face (Refuse, mv 4) is restricted; the
	// AFTERMATH face (Cooperate, mv 3) is not.
	if !e.castRestricted(0, id) {
		t.Fatal("precondition: Refuse (mv 4) is not restricted by Gaddock Teeg's " +
			"Card.nonCreature+cmcGE4 static")
	}
	if probeFaceRestricted(t, e, id, af) {
		t.Fatal("precondition: Cooperate (mv 3) is restricted too, so the fixture is vacuous")
	}

	addMana(t, e, 0, "RUUU")
	leaveSpellOnStackAtOpponent(t, e, bolt)
	am := aftermathOption(t, e, id)
	if am == nil || am.Label != "Cast Cooperate (aftermath)" {
		t.Fatalf("aftermath withheld although only the front face is restricted: %+v",
			castOptions(t, e))
	}
	submitChoices(t, e, am.Index)
	so := e.G.Obj(id)
	if so.Zone != state.ZStack || int(so.FaceIdx) != 1 || so.Face().Name != "Cooperate" {
		t.Fatalf("aftermath did not select Cooperate: zone=%s faceIdx=%d name=%q",
			so.Zone, so.FaceIdx, so.Face().Name)
	}
	// Cooperate's own target ask (ValidTgts$ Instant,Sorcery -- the Bolt on the
	// stack). Answering it lets the transaction complete and stamp the
	// aftermath flag.
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		tgt := -1
		for _, o := range d.Options {
			if o.Obj == bolt {
				tgt = o.Index
			}
		}
		if tgt < 0 {
			t.Fatalf("precondition: Cooperate offered no option for the Bolt on the stack: %+v",
				d.Options)
		}
		submitChoices(t, e, tgt)
	}
	if so = e.G.Obj(id); so.CastFlags&state.FlagAftermath == 0 {
		t.Fatalf("aftermath cast did not stamp FlagAftermath: flags=%+v", so.CastFlags)
	}
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(id).Zone; z != state.ZExile {
		t.Fatalf("aftermath spell went to %s, want exile", z)
	}
	replayCheck(t, e, cfg)
}

// TestAlternateFaceCantBeCastAftermathBackRestricted: Gaddock Teeg's
// Card.nonCreature+cmcGE4 matches Oblivion (mana value 5, the K:Aftermath
// half) but not Consign (mana value 2), so Consign // Oblivion resting in the
// graveyard must NOT offer the prohibited aftermath cast (the old card-level
// gate read the unrestricted front and offered it).
func TestAlternateFaceCantBeCastAftermathBackRestricted(t *testing.T) {
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Consign", "Gaddock Teeg")
	id := searchMoveByName(t, e, "Consign", state.ZGraveyard)
	moveRestrictionSource(t, e, "Gaddock Teeg")
	o := e.G.Obj(id)
	if o.Zone != state.ZGraveyard || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: Consign zone=%s faceIdx=%d, want graveyard/front",
			o.Zone, o.FaceIdx)
	}
	af := aftermathAlternateFace(o)
	if af == nil || af.Name != "Oblivion" {
		t.Fatalf("precondition: aftermath alternate face is %v, want Oblivion", af)
	}
	// Precondition: the FRONT face (Consign, mv 2) is unrestricted; the
	// AFTERMATH face (Oblivion, mv 5) IS restricted.
	if e.castRestricted(0, id) {
		t.Fatal("precondition: Consign (mv 2) is restricted, so the front face is " +
			"not the unrestricted control this direction needs")
	}
	if !probeFaceRestricted(t, e, id, af) {
		t.Fatal("precondition: Oblivion (mv 5) is not restricted by Gaddock Teeg's " +
			"Card.nonCreature+cmcGE4 static")
	}

	// Fund the AFTERMATH half's own cost ({4}{B}) as well as the front's, so
	// the only gate that can withhold the aftermath offer is the restriction
	// -- an unaffordable half would pass the test for the wrong reason.
	addMana(t, e, 0, "BBBBB")
	if am := aftermathOption(t, e, id); am != nil {
		t.Fatalf("aftermath offered for a face the CantBeCast static prohibits: %+v", am)
	}
	replayCheck(t, e, cfg)
}
