package main

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestAutoPayRunBuildsManualSeatPlans pins the class the lazy drive loop must
// cover: the apProbe reads d.PaymentActions for EVERY deciding seat, so an
// auto-pay run is itself a consumer of the manual (non-auto-pay) seats'
// extension -- manual_seat_priority_with_plan measures plans offered to seats
// that ignore them. EnsurePaymentActions is only built at cmd/cardfuzz's
// single guard site, so if that guard stops covering non-consumer seats in an
// auto-pay run this counter falls to zero while the auto-pay seats' own
// counters stay healthy (TestAutoPayGameSubmitsPlans only checks those).
//
// explore=true leaves the explore seat MANUAL in "all" mode, so the run holds
// exactly one non-consumer seat whose offered plans the probe counts.
func TestAutoPayRunBuildsManualSeatPlans(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var d genDeck
	d.Colour = "G"
	for i := 0; i < 60; i++ {
		switch {
		case i < 24:
			d.Cards = append(d.Cards, "Forest")
		case i < 36:
			d.Cards = append(d.Cards, "Llanowar Elves")
		case i < 50:
			d.Cards = append(d.Cards, "Grizzly Bears")
		default:
			d.Cards = append(d.Cards, "Hill Giant")
		}
	}
	decks := []genDeck{d, d}
	all, _ := parseAutoPay("all", false)
	f, gc := playGame(reg, decks, 9, 14, 20000, 0, true, true, all)
	if f != nil {
		t.Fatalf("auto-pay game failed: %s %s", f.Kind, f.Diag)
	}
	// Precondition: the explore seat really is the manual one this run, and it
	// really reached a priority decision -- otherwise the assertion below
	// could pass or fail on an empty counter for the wrong reason.
	if gc.ap.Priority == 0 {
		t.Fatalf("setup: no auto-pay seat reached priority: %s", gc.ap)
	}
	if gc.ap.ManualSeatPriority == 0 {
		t.Fatalf("setup: the explore seat saw no priority decision: %s", gc.ap)
	}
	if gc.ap.ManualSeatPriorityPlan == 0 {
		t.Fatalf("manual seat saw no offered plan in an auto-pay run: %s", gc.ap)
	}
}
