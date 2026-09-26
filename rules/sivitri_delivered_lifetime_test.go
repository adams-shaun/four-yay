// The lifetime half of Sivitri, Dragon Master's delivered CantAttackUnless.
//
// bb2f5ec9d closed both omissions (effEffect's restriction switch and
// attackPairCharge's delivered walk). rules/delivered_cantattackunless_test.go
// proves the charge end to end and that the tax is gone once seat 0's next
// turn begins, but it reads only the registry ENTRY COUNT at the boundary.
// This file pins the registration's actual lifetime fields -- Duration$
// "UntilYourNextTurn" and a frozen, strictly-future UntilTurn -- so a
// regression that registers the tax as Permanent (or as this-turn UntilEOT)
// fails at the registration site rather than only after a full turn drive.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestSivitriDeliveredCantAttackUnlessLifetimePinned activates the real
// corpus Sivitri +1 and asserts the registered restriction carries the
// explicit UntilYourNextTurn lifetime, not a Permanent or UntilEOT default.
func TestSivitriDeliveredCantAttackUnlessLifetimePinned(t *testing.T) {
	sivCard := mshCorpusCard(t, "Sivitri, Dragon Master")
	e := chargeEngine(t, 9106, sivCard)
	activateSivitriPlusOne(t, e, sivCard)

	// Precondition: exactly one delivered CantAttackUnless is live, so the
	// field reads below name Sivitri's +1 and not an unrelated effect.
	var got []state.ContinuousEffect
	for _, ce := range e.active() {
		if ce.Restriction == "CantAttackUnless" {
			got = append(got, ce)
		}
	}
	if len(got) != 1 {
		t.Fatalf("precondition: %d delivered CantAttackUnless registrations, want 1", len(got))
	}
	ce := got[0]

	// The explicit Duration$ survives onto the registration. Sivitri's script
	// writes Duration$ UntilYourNextTurn; a Permanent reading would outlive
	// the turn the card names, and an UntilEOT reading would end too early.
	if ce.Duration != "UntilYourNextTurn" {
		t.Fatalf("registered lifetime Duration = %q, want UntilYourNextTurn", ce.Duration)
	}
	// A real turn boundary was frozen at registration: UntilYourNextTurn is a
	// START boundary, so it expires as the controller's next turn begins,
	// which is strictly after the turn now in progress. Zero means "no turn
	// boundary" (UntilEOT / source-leaves), the wrong lifetime for this card.
	if ce.UntilTurn <= e.G.Turn {
		t.Fatalf("registered UntilTurn = %d, want a future boundary > current turn %d", ce.UntilTurn, e.G.Turn)
	}
	if ce.UntilEOT {
		t.Fatalf("registered UntilEOT = true, want false (this is a next-turn boundary, not end of turn)")
	}
}
