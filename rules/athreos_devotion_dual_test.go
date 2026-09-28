package rules

// Athreos, Shroud-Veiled is the corpus's canonical Count$DevotionDual carrier:
//
//	S:Mode$ Continuous | Affected$ Card.Self | RemoveType$ Creature | CheckSVar$ X | SVarCompare$ LT7
//	SVar:X:Count$DevotionDual.White.Black
//
// The count head was fixed by f89ff4cd7 ("feat(effects): Count$Threshold and
// Count$Devotion count heads"). The generic two-colour arithmetic is pinned by
// effects/count_devotion_threshold_test.go's TestDevotionDualSumsBothColours;
// this file pins that Athreos's REAL compiled script shape keeps reaching it,
// through the public engine surface (rules.Engine implements effects.Host via
// Game()). It exercises ONLY the SVar count -- Athreos's RemoveType$ static is
// still independently unread (rules/paramcensus_test.go's knownUnsupportedParams
// holds the sibling RemoveType entries) and is deliberately not asserted here.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// athreosShape asserts the audited corpus shape and returns the SVar body. The
// assertions ride on the exact static guard and SVar spelling, so a corpus pin
// change that hollows the pin out fails here rather than passing vacuously.
func athreosShape(t *testing.T, athreos *cards.Card) string {
	t.Helper()
	f := athreos.Faces[0]
	var dual *cards.Static
	for i := range f.Statics {
		s := &f.Statics[i]
		if s.Mode == "Continuous" && s.Params["Affected"] == "Card.Self" {
			dual = s
		}
	}
	if dual == nil {
		t.Fatalf("corpus Athreos carries no Continuous Card.Self static: %+v", f.Statics)
	}
	if got := dual.Params["RemoveType"]; got != "Creature" {
		t.Fatalf("Athreos static RemoveType = %q, want Creature (params %v)", got, dual.Params)
	}
	if got := dual.Params["CheckSVar"]; got != "X" {
		t.Fatalf("Athreos static CheckSVar = %q, want X (params %v)", got, dual.Params)
	}
	if got := dual.Params["SVarCompare"]; got != "LT7" {
		t.Fatalf("Athreos static SVarCompare = %q, want LT7 (params %v)", got, dual.Params)
	}
	body := f.SVars["X"]
	if body != "Count$DevotionDual.White.Black" {
		t.Fatalf("corpus Athreos SVar:X = %q, want Count$DevotionDual.White.Black", body)
	}
	return body
}

// TestAthreosShroudVeiledDevotionDual pins the boundary the static guard reads:
// Athreos's own {4}{W}{B} contributes one white and one black pip, a controlled
// four-pip W/B fixture brings the sum to six (still LT7, so the guard holds),
// and one more white pip brings it to seven (the guard's boundary). Both reads
// go through EvalCountOK with the corpus card's own SVar body.
func TestAthreosShroudVeiledDevotionDual(t *testing.T) {
	t.Parallel()
	athreos := corpusCard(t, "Athreos, Shroud-Veiled")
	body := athreosShape(t, athreos)

	e := layerEngine(t)
	athreosID := onBoardCard(t, e, 0, athreos)

	// Precondition: the corpus static is actually attached to the object on
	// seat 0's battlefield, and the SVar body we evaluate is that object's.
	if got := e.G.Obj(athreosID).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: Athreos zone = %v, want ZBattlefield", got)
	}
	if got := e.G.Obj(athreosID).Face().SVars["X"]; got != body {
		t.Fatalf("precondition broken: battlefield Athreos SVar:X = %q, want %q", got, body)
	}

	ctx := &effects.Ctx{Controller: 0, Source: athreosID}

	// Athreos alone: {4}{W}{B} is one white + one black = 2.
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("DevotionDual with Athreos alone = (%d, %v), want (2, true)", n, ok)
	}

	// A controlled fixture with four white/black pips: {W}{W}{B}{B}.
	// 2 white + 2 black on top of Athreos's 1+1 gives 6 -- one short of the
	// LT7 boundary.
	four := onBoardCard(t, e, 0, card(t, "Name:FourPip Idol\nManaCost:W W B B\nTypes:Creature Spirit\nPT:1/1\nOracle:x\n"))
	if got := e.G.Obj(four).Zone; got != state.ZBattlefield {
		t.Fatalf("precondition broken: four-pip fixture zone = %v, want ZBattlefield", got)
	}
	fourPips := e.G.Obj(four).Face().ManaCost
	if fourPips != "W W B B" {
		t.Fatalf("precondition broken: fixture ManaCost = %q, want %q", fourPips, "W W B B")
	}

	n6, ok6 := effects.EvalCountOK(e, ctx, body)
	if !ok6 || n6 != 6 {
		t.Fatalf("DevotionDual with Athreos + four W/B pips = (%d, %v), want (6, true)", n6, ok6)
	}
	// The guard's LT7 must still hold at six, or the boundary below proves
	// nothing.
	if n6 >= 7 {
		t.Fatalf("boundary precondition broken: six-pip board already meets LT7 with %d", n6)
	}

	// One more controlled white pip: 7, the LT7 boundary.
	one := onBoardCard(t, e, 0, card(t, "Name:OnePip Wanderer\nManaCost:W\nTypes:Creature Cleric\nPT:1/1\nOracle:x\n"))
	if e.G.Obj(one).Zone != state.ZBattlefield || e.G.Obj(one).Face().ManaCost != "W" {
		t.Fatalf("precondition broken: one-pip fixture not a battlefield {W} object")
	}
	n7, ok7 := effects.EvalCountOK(e, ctx, body)
	if !ok7 || n7 != 7 {
		t.Fatalf("DevotionDual with Athreos + four W/B pips + one white pip = (%d, %v), want (7, true)", n7, ok7)
	}
	// And the boundary is real: seven is no longer LT7.
	if n7 < 7 {
		t.Fatalf("boundary precondition broken: seven-pip board is not at the LT7 boundary (%d)", n7)
	}
}
