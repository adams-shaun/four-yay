package rules

// Q3a follow-up: a face that prints a graveyard-recast head AND Delve has a
// real recast floor below its printed one. offerCastableUsing credits Delve on
// EVERY cast scope (rules/mana.go reads hasKeywordH(id, kwhDelve) and
// subtracts one generic per graveyard card), including the flashback/mayhem/
// warp loops that pass the raw recast cost without castOfferBase. The proof
// must therefore call the route open, or it would skip a window whose walk
// really offers the recast. These rows pin that: the walk offers the flashback
// (so the row is not vacuous) and the proof blocks it.
//
// The precondition assertions (the printed floor is above the ceiling, the
// facts mark the route open) make a fixture that stopped exercising Delve fail
// loudly instead of passing on the general "above the ceiling is quiet" path.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// quietFlashbackDelve is a {1}{U} instant with flashback {2}{U} (printed floor
// 3) and Delve, so the offer gate can credit the graveyard against the
// flashback's generic.
func quietFlashbackDelve(t testing.TB) *cards.Card {
	return card(t, "Name:Quiet Recast Delve\nManaCost:1 U\nTypes:Instant\n"+
		"K:Flashback:2 U\nK:Delve\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
}

// TestQuietRecastDelveOpensRoute: a flashback whose printed floor is above the
// ceiling is still offered by the walk when Delve credits the graveyard, so
// the proof must block (open), not call the window quiet.
func TestQuietRecastDelveOpensRoute(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bears := lookup(t, reg, "Grizzly Bears")
	fb := quietFlashbackDelve(t)
	// Printed floor 3 ({2}{U}); ceiling 1 (one blue). Without the Delve credit
	// the walk could not pay {2}{U}. Graveyard size 3 (the flashback card plus
	// two bears) credits all two generic, leaving {U} payable at the ceiling.
	e := quietBasePool(t, reg, 1, []*cards.Card{fb, bears, bears})
	id := addZone(t, e, 0, fb, state.ZGraveyard)
	addZone(t, e, 0, bears, state.ZGraveyard)
	addZone(t, e, 0, bears, state.ZGraveyard)

	ff := e.walkFaceFactsOf(e.G.Obj(id).Face())
	if ff == nil || !ff.quiet.recastOpen {
		t.Fatalf("precondition: flashback+delve facts are %+v, want recastOpen", ff)
	}
	if ff.quiet.recastFloor != -1 {
		t.Fatalf("precondition: recastFloor = %d, want -1 for an open route", ff.quiet.recastFloor)
	}
	ceiling, _, _ := e.quietManaCeiling(0)
	if ceiling >= 3 {
		t.Fatalf("precondition: ceiling %d is not below the printed floor 3", ceiling)
	}
	// The route is real: the walk offers the flashback because Delve credits
	// the graveyard. This is the control that the window is not vacuous.
	e.priorityRound()
	if !hasMode(e.legalActions(0), "flashback") {
		t.Fatalf("precondition: the walk does not offer the Delve-credited flashback: %v", optKinds(e.legalActions(0)))
	}
	if got := e.quietBlocker(0); got != qbGraveRoute {
		t.Fatalf("quietBlocker = %s, want %s (a Delve-credited flashback is open and blocks)",
			quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
	}
	if e.seatQuiet(0) {
		t.Fatal("the proof called quiet a window whose walk offers a Delve-credited flashback")
	}
}

// TestQuietRecastDelveWithoutGraveyardBlocks is the sibling direction: the
// printed face still carries Delve, so the route is open even when the
// graveyard is too small for the credit to matter. The proof is a pure
// function of the face and cannot read the graveyard, so it must fail closed.
func TestQuietRecastDelveWithoutGraveyardBlocks(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	fb := quietFlashbackDelve(t)
	// Ceiling 1; only the flashback card itself is in the graveyard, so the
	// walk cannot pay the printed {2}{U}. The proof still blocks (open).
	e := quietBasePool(t, reg, 1, []*cards.Card{fb})
	id := addZone(t, e, 0, fb, state.ZGraveyard)
	ff := e.walkFaceFactsOf(e.G.Obj(id).Face())
	if ff == nil || !ff.quiet.recastOpen {
		t.Fatalf("precondition: flashback+delve facts are %+v, want recastOpen", ff)
	}
	if got := e.quietBlocker(0); got != qbGraveRoute {
		t.Fatalf("quietBlocker = %s, want %s (a Delve face fails closed even with a small graveyard)",
			quietBlockerNames[got], quietBlockerNames[qbGraveRoute])
	}
}
