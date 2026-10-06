// Count$LastStateBattlefieldWithFallback — the battlefield count read from
// the last known-state snapshot with a current-battlefield fallback (ticket
// cli-20261006T024354Z-3345d0e0, Count$ heads part 2). OTJ Steer Clear gates
// its four-damage branch on `SVar:Y:Count$LastStateBattlefieldWithFallback
// Permanent.Mount+YouCtrl` ("if you controlled a Mount as you cast this
// spell"). Before this head existed the body was the unresolvable zero, so
// `SVar:X:Count$Compare Y GE1.4.2` always took the 2-damage branch and the
// card reported as unsupported [count:LastStateBattlefieldWithFallback].
//
// This build carries no last-state snapshot, so the read is the fallback:
// the CURRENT battlefield, through the one Valid zone scan. The pin runs on
// Steer Clear's real compiled face and proves the head distinguishes a Mount
// from a non-Mount and Your control from an opponent's.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
)

// TestLastStateBattlefieldHeadOnSteerClear pins the head against Steer
// Clear's real SVar Y and its Compare branch.
func TestLastStateBattlefieldHeadOnSteerClear(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	steer := onBoardCard(t, e, 0, corpusCard(t, "Steer Clear"))
	face := e.G.Obj(steer).Face()

	body, ok := face.SVars["Y"]
	if !ok || body != "Count$LastStateBattlefieldWithFallback Permanent.Mount+YouCtrl" {
		t.Fatalf("test precondition: Steer Clear SVar Y = %q (ok %v)", body, ok)
	}
	ctx := &effects.Ctx{Controller: 0, Source: steer, SVars: face.SVars}

	// Precondition: the head is MODELLED (ok true), not the unresolvable
	// fallthrough. This is also the fix's contract: before the arm existed
	// EvalCountOK returned (0, false) here.
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("LastStateBattlefieldWithFallback (no Mount) = %d (ok %v), want evaluated 0", n, ok)
	}

	// A non-Mount permanent must NOT count: the filter is doing real work,
	// not counting every permanent.
	onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("LastStateBattlefieldWithFallback (non-Mount only) = %d (ok %v), want 0", n, ok)
	}

	// A Mount the controller controls counts.
	onBoardCard(t, e, 0, corpusCard(t, "Venomsac Lagac"))
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("LastStateBattlefieldWithFallback (one own Mount) = %d (ok %v), want 1", n, ok)
	}

	// An OPPONENT's Mount must not count: the +YouCtrl qualifier binds.
	onBoardCard(t, e, 1, corpusCard(t, "Venomsac Lagac"))
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 1 {
		t.Fatalf("LastStateBattlefieldWithFallback (own + opponent Mount) = %d (ok %v), want 1", n, ok)
	}

	// The carrier's actual branch: with Y >= 1 the Compare answers 4, else 2.
	if n, ok := effects.EvalCountOK(e, ctx, "Count$Compare Y GE1.4.2"); !ok || n != 4 {
		t.Fatalf("Steer Clear X branch (Y>=1) = %d (ok %v), want 4", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$LastStateBattlefieldWithFallback Faerie.YouCtrl"); !ok || n != 0 {
		t.Fatalf("LastStateBattlefieldWithFallback (empty Faerie filter) = %d (ok %v), want evaluated 0", n, ok)
	}
}
