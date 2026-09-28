package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// villageRitesSrc is the real corpus script for Village Rites: an instant
// whose SpellAbility carries an ADDITIONAL cost (Sac<1/Creature>) on top of
// its printed {B}. It is the card that wedged a live 4-player game.
const villageRitesSrc = "Name:Village Rites\nManaCost:B\nTypes:Instant\n" +
	"A:SP$ Draw | Cost$ B Sac<1/Creature> | NumCards$ 2 | SpellDescription$ Draw two cards.\n" +
	"Oracle:As an additional cost to cast this spell, sacrifice a creature.\\nDraw two cards.\n"

const bearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// plainCastOffered reports whether p's legal actions include a PLAIN cast of
// obj (no alternative cost, no keyword mode) -- the offer this file is about.
// cast_liveness_test.go's castOffered reads the pending decision instead; this
// one asks the offer walk directly, so it does not depend on whose priority it
// currently is.
func plainCastOffered(e *Engine, p state.PlayerID, obj state.ObjID) bool {
	for _, o := range e.legalActions(p) {
		if o.Kind == "cast" && o.Obj == obj && o.Mode == "" && o.AltCostIndex == 0 {
			return true
		}
	}
	return false
}

// TestAdditionalSacrificeCostGatesTheOffer is the regression for the live
// livelock: a spell whose SpellAbility Cost$ carries an additional
// Sac<1/Creature> must NOT be offered to a player with no creature.
//
// The engine used to gate the plain-cast offer on the printed mana alone,
// while beginCast folded the additional sacrifice in afterwards. Village Rites
// was therefore offered to a player holding {B} and no creature; sacAsk found
// no candidates and aborted the cast. The abort is correct and consumes
// nothing -- which is exactly what made it unbounded: priority returned to the
// same board that produced the offer, so the option came back, was taken, and
// aborted again forever. A real 4-player game sat on turn 3 emitting the same
// five events until it was killed.
//
// The oracle is about the OFFER, not the abort: an option that cannot be paid
// must never be offered (CR 601.2b -- you announce a spell only if you can
// take every step, and 601.2f-h price and pay the total cost, additional costs
// included).
func TestAdditionalSacrificeCostGatesTheOffer(t *testing.T) {
	t.Parallel()
	e, _, rites := newFixtureDeck(t, 30, villageRitesSrc, bearSrc)
	addMana(t, e, 0, "B")

	// No creature on the battlefield: the additional cost cannot be paid, so
	// the cast must not be on the menu at all.
	if plainCastOffered(e, 0, rites) {
		t.Fatal("Village Rites offered with no creature to sacrifice: an unpayable additional cost was offered, which livelocks on abort")
	}

	// The gate must not simply refuse the card forever: give the player a
	// creature and the same cast becomes legal. Without this half, deleting
	// the offer entirely would pass.
	bear := putCreature(t, e, 0, bearSrc)
	if !plainCastOffered(e, 0, rites) {
		t.Fatal("Village Rites NOT offered with a creature on the battlefield: the gate is refusing a payable cost")
	}
	_ = bear
}

// TestUnpayableSacrificeAbortHoldsTheOptionOut pins the LIVENESS half, on the
// abort path itself so it does not depend on the offer gate above.
//
// An abort that consumes nothing returns priority to the exact board that
// produced the offer. If that board still offers the option, the seat takes it
// again and aborts again, forever -- which is what a live 4-player game did on
// turn 3 until it was killed. The engine already has the answer for this
// (suppressedCast: hold the option out for the rest of the priority window,
// lift it on the first state-changing event -- see cast_liveness_test.go for
// the Delve decline), but this abort site hand-rolled its teardown and never
// engaged it. Ruling F05-2 (CR 733.2) delays the hold-out to the SECOND
// identical no-progress abort, so a single abort leaves the option offered as
// a legal retry.
//
// Reaching the abort needs a hand-built pendingCast now that the offer gate
// refuses to hand one out, exactly as TestUnderDelveAbortsTheCast does for the
// under-delve abort.
func TestUnpayableSacrificeAbortHoldsTheOptionOut(t *testing.T) {
	t.Parallel()
	e, _, rites := newFixtureDeck(t, 30, villageRitesSrc, bearSrc)
	addMana(t, e, 0, "B")

	// Seat 0 holds no creature, so the Sac part has zero candidates.
	e.cast = &pendingCast{player: 0, card: rites, from: state.ZHand, ability: -1,
		cost: ParseCost("B Sac<1/Creature>")}
	e.continueCast()

	if !hasNote(e, "sacrifice cost no longer payable") {
		t.Fatal("no abort Note: the unpayable sacrifice did not abort")
	}
	if e.cast != nil || e.choosing != chooseNone {
		t.Fatal("cast flow not cleared after abort")
	}
	if e.G.Obj(rites).Zone != state.ZHand {
		t.Fatalf("Village Rites in %s, want hand (an aborted cast moves nothing)", e.G.Obj(rites).Zone)
	}
	// CR 733.2: the FIRST no-progress abort leaves the option offered, so a
	// reversed illegal action may be redone legally -- the identical board
	// re-offers the doomed cast once, which is exactly the retry the ruling
	// wants, not yet a livelock.
	if e.castSuppressed(0, rites) {
		t.Fatal("a first no-progress sacrifice abort wrongly held the option out; CR 733.2 allows a legal retry")
	}

	// The SECOND identical no-progress abort is the hold-out: the identical
	// board cannot re-offer the doomed cast and spin.
	e.cast = &pendingCast{player: 0, card: rites, from: state.ZHand, ability: -1,
		cost: ParseCost("B Sac<1/Creature>")}
	e.continueCast()
	if !e.castSuppressed(0, rites) {
		t.Fatal("option not held out after a second no-progress abort: the same board will re-offer the same doomed cast, which is an unbounded livelock")
	}
}
