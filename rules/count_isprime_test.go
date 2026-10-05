// Count$IsPrime — the arithmetic branch head (ticket
// cli-20261005T092134Z-fed167ca, part 1). DSK Zimone, All-Questioning gates
// its end-step token on `SVar:X:Count$IsPrime Y.Z.0`: X is nonzero only when
// Y (the number of lands its controller controls) is prime, and then it
// carries Z (the number that entered this turn). Before this head existed the
// body degraded to the unresolvable zero, so the intervening-if gate failed
// open (per the count-head convention) and X was never a meaningful value.
//
// The pin runs on Zimone's real compiled face. Two Y values with different
// primality prove the branch actually turns on primality rather than on the
// operand it happens to read.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestIsPrimeHeadOnZimone pins Forge's Count$IsPrime <SVar>.<True>.<False>
// against Zimone, All-Questioning's real SVar X. 7 (prime) must answer Z, 8
// (not prime) must answer the literal 0.
func TestIsPrimeHeadOnZimone(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	zimone := onBoardCard(t, e, 0, corpusCard(t, "Zimone, All-Questioning"))
	face := e.G.Obj(zimone).Face()

	// Precondition: the corpus face carries the body this ticket implements,
	// and the head is MODELLED (ok true), not the unresolvable fallthrough.
	body, ok := face.SVars["X"]
	if !ok || body != "Count$IsPrime Y.Z.0" {
		t.Fatalf("test precondition: Zimone SVar X = %q (ok %v), want Count$IsPrime Y.Z.0", body, ok)
	}
	ctx := &effects.Ctx{Controller: 0, Source: zimone, SVars: face.SVars}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline IsPrime (0 lands) = %d (ok %v), want evaluated 0", n, ok)
	}

	// Seven lands in play (Y = 7, prime) and one that entered this turn
	// (Z = 1): the head must answer Z.
	for i := 0; i < 6; i++ {
		onBoardCard(t, e, 0, corpusCard(t, "Forest"))
	}
	// Bind the seventh from hand through a real MoveZone so the
	// ThisTurnEntered fold records it, then assert both operands of the
	// branch precondition rather than trusting the setup.
	seventh := corpusCard(t, "Forest")
	so := e.G.AddObject(seventh, 0)
	so.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), so.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: so.ID, From: state.ZHand, To: state.ZBattlefield})
	if n, ok := effects.EvalCountOK(e, ctx, "Count$Valid Land.YouCtrl+!IsRemembered"); !ok || n != 7 {
		t.Fatalf("precondition: land count Y = %d (ok %v), want 7", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$ThisTurnEntered_Battlefield_Land.YouCtrl"); !ok || n != 1 {
		t.Fatalf("precondition: lands entered this turn Z = %d (ok %v), want 1", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("IsPrime with prime Y=7 = %d (ok %v), want 1 (the true branch Z)", n, ok)
	}

	// An eighth land makes Y = 8, not prime: the false branch (literal 0)
	// answers regardless of Z. Assert Y=8 first so a setup slip fails loudly.
	onBoardCard(t, e, 0, corpusCard(t, "Forest"))
	if n, ok := effects.EvalCountOK(e, ctx, "Count$Valid Land.YouCtrl+!IsRemembered"); !ok || n != 8 {
		t.Fatalf("precondition: land count Y = %d (ok %v), want 8", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("IsPrime with non-prime Y=8 = %d (ok %v), want 0 (the false branch)", n, ok)
	}
}
