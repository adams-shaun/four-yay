package rules

// fuse_cantbecast_test.go pins the CR 709.5 prohibition gate on a Fuse cast
// (task fuse-cantbecast): a fused cast is ONE spell with BOTH halves'
// characteristics, so a CantBeCast restriction that matches EITHER half must
// withhold the fused offer.
//
// Before this change the hand fuse offer (rules/legal.go) sat below the
// front-face `castRestricted` continue and probed only that front face. A
// restriction matching only the ALTERNATE half (Gaddock Teeg's
// Card.nonCreature+cmcGE4 against Breaking // Entering's Entering, mv 6)
// therefore offered `Cast Breaking // Entering (fused)` while the front half
// (Breaking, mv 2) was unrestricted. The front-only direction was already
// withheld by the continue and is the preservation control here.
//
// Corpus cards only -- no Forge script text is committed here.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// fuseTestEngine deals seat 0 a 40-card deck led by the named fuse carrier
// (and, when teeg is true, Gaddock Teeg, which moveRestrictionSource then
// event-sources onto the battlefield) and returns the engine, its Config and
// the carrier's id in seat 0's hand. The carrier sits at its front face.
func fuseTestEngine(t *testing.T, reg *cards.Registry, carrier string, teeg bool, seed uint64) (*Engine, Config, state.ObjID) {
	t.Helper()
	card := searchCorpusCard(t, reg, carrier)
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{card}
	if teeg {
		deck = append(deck, searchCorpusCard(t, reg, "Gaddock Teeg"))
	}
	for len(deck) < 40 {
		deck = append(deck, island)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"fuse", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	if teeg {
		moveRestrictionSource(t, e, "Gaddock Teeg")
	}
	id := searchMoveByName(t, e, carrier, state.ZHand)
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || int(o.FaceIdx) != 0 {
		t.Fatalf("precondition: %s zone=%v faceIdx=%d, want hand/front", carrier, o, o.FaceIdx)
	}
	return e, cfg, id
}

// TestFuseCantBeCastEitherHalf is the brief's pair of directions on two real
// corpus fuse carriers:
//
//  1. Breaking // Entering -- front (Breaking, mv 2) UNRESTRICTED, alternate
//     (Entering, mv 6) restricted under Gaddock Teeg. The fused offer must be
//     absent while Breaking's ordinary front offer remains; with Teeg absent
//     the same funded card DOES offer the fused cast (the control that proves
//     the absence is the restriction and not an unaffordable cost).
//  2. Alive // Well -- front (Alive, mv 4) restricted, alternate (Well, mv 1)
//     unrestricted. The fused offer is absent (the front-face continue) while
//     Well's independent split_alt offer remains.
func TestFuseCantBeCastEitherHalf(t *testing.T) {
	reg := searchTestRegistry(t)

	t.Run("BreakingEnteringBackRestricted", func(t *testing.T) {
		e, cfg, id := fuseTestEngine(t, reg, "Breaking", true, 8421)
		o := e.G.Obj(id)
		ff, fa := fusedSplitFaces(o)
		if ff == nil || fa == nil || ff.Name != "Breaking" || fa.Name != "Entering" {
			t.Fatalf("precondition: fused faces = %v/%v, want Breaking/Entering", ff, fa)
		}
		// Precondition: the restriction must actually differ between the two
		// faces, and must match the ALTERNATE (back) half -- otherwise the
		// direction is unobservable and a missing offer proves nothing.
		if e.castRestricted(0, id) {
			t.Fatal("precondition: Breaking (mv 2) is restricted by Gaddock Teeg, " +
				"so the front half is not the unrestricted control")
		}
		if !probeFaceRestricted(t, e, id, fa) {
			t.Fatal("precondition: Entering (mv 6) is not restricted by Gaddock " +
				"Teeg's Card.nonCreature+cmcGE4 static")
		}
		// Combined cost {U}{B}+{4}{B}{R} = {4}{U}{B}{B}{R} (8 mana); fund it plus a
		// spare so affordability is never the withholding reason.
		addMana(t, e, 0, "UUBBRGGG")
		if fuse := splitOption(t, e, id, "fuse"); fuse != nil {
			t.Fatalf("fused offer present although the back half is prohibited: %+v", fuse)
		}
		plain := splitOption(t, e, id, "")
		if plain == nil || plain.Label != "Cast Breaking" {
			t.Fatalf("Breaking's ordinary front offer withheld although unrestricted: %+v",
				castOptions(t, e))
		}
		// Control: the SAME funded carrier offers the fused cast with Teeg
		// absent. (Separate engine, so the mana funding is reapplied.)
		ce, _, cid := fuseTestEngine(t, reg, "Breaking", false, 8421)
		addMana(t, ce, 0, "UUBBRGGG")
		cf := splitOption(t, ce, cid, "fuse")
		if cf == nil || cf.Label != "Cast Breaking // Entering (fused)" {
			t.Fatalf("control: fused offer missing without the restriction: %+v",
				castOptions(t, ce))
		}
		replayCheck(t, e, cfg)
	})

	// EnforcementRecheck pins that the CR 601.2e recheck (cast.go's
	// recheckIllegal) runs the SAME both-halves rule as the offer. The offer
	// gate makes the prohibited fused cast unreachable through the normal
	// flow, so this asserts the enforcement path directly: a pending fuse
	// proposal of a front-unrestricted / back-restricted carrier must be
	// judged illegal (true = reversed), not wave through on the front face's
	// mana value the way the pre-fix recheck did.
	t.Run("EnforcementRecheck", func(t *testing.T) {
		e, _, id := fuseTestEngine(t, reg, "Breaking", true, 8423)
		// Front unrestricted is the precondition that makes the OLD front-only
		// recheck return false; without it the test could pass on the old code
		// through the front half and prove nothing about the back-half rule.
		if e.castRestricted(0, id) {
			t.Fatal("precondition: Breaking's front half is restricted, so the " +
				"front-only recheck would also reverse the proposal")
		}
		if _, fa := fusedSplitFaces(e.G.Obj(id)); fa == nil || !probeFaceRestricted(t, e, id, fa) {
			t.Fatal("precondition: Entering is not restricted under the probe")
		}
		pc := &pendingCast{player: 0, card: id, mode: "fuse", ability: -1}
		if !e.recheckIllegal(pc) {
			t.Fatal("recheckIllegal passed a fused proposal whose back half is prohibited " +
				"(front-only mana value read)")
		}
	})

	t.Run("AliveWellFrontRestricted", func(t *testing.T) {
		e, cfg, id := fuseTestEngine(t, reg, "Alive", true, 8422)
		o := e.G.Obj(id)
		ff, fa := fusedSplitFaces(o)
		if ff == nil || fa == nil || ff.Name != "Alive" || fa.Name != "Well" {
			t.Fatalf("precondition: fused faces = %v/%v, want Alive/Well", ff, fa)
		}
		// Precondition: the FRONT half is restricted and the alternate is not
		// -- the opposite direction from the subtest above.
		if !e.castRestricted(0, id) {
			t.Fatal("precondition: Alive (mv 4) is not restricted by Gaddock Teeg's " +
				"Card.nonCreature+cmcGE4 static")
		}
		if probeFaceRestricted(t, e, id, fa) {
			t.Fatal("precondition: Well (mv 1) is restricted too, so the fixture is vacuous")
		}
		// Combined cost {3}{G}+{W} = {3}{G}{W}; fund it plus a spare.
		addMana(t, e, 0, "GGWWW")
		if fuse := splitOption(t, e, id, "fuse"); fuse != nil {
			t.Fatalf("fused offer present although the front half is prohibited: %+v", fuse)
		}
		alt := splitOption(t, e, id, "split_alt")
		if alt == nil || alt.Label != "Cast Well" {
			t.Fatalf("Well's independent offer withheld although unrestricted: %+v",
				castOptions(t, e))
		}
		// Control: with Teeg absent the fused cast is offered.
		ce, _, cid := fuseTestEngine(t, reg, "Alive", false, 8422)
		addMana(t, ce, 0, "GGWWW")
		cf := splitOption(t, ce, cid, "fuse")
		if cf == nil || cf.Label != "Cast Alive // Well (fused)" {
			t.Fatalf("control: fused offer missing without the restriction: %+v",
				castOptions(t, ce))
		}
		replayCheck(t, e, cfg)
	})
}
