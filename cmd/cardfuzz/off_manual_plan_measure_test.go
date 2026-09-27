package main

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// planMeasurementFixture is the fixed two-deck green fixture the auto-pay
// tests share: one 60-card mono-green deck whose every cast (Forest, Llanowar
// Elves, Grizzly Bears, Hill Giant) is a V1-plan shape, played in both seats.
func planMeasurementFixture() []genDeck {
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
	return []genDeck{d, d}
}

// TestOffModeManualSeatPlanMeasurement pins BOTH off-mode cases of the
// manual_seat_priority_with_plan diagnostic:
//
//   - default -autopay off: lazy publication builds no seat's payment
//     extension, so no plan is ever on offer and the counter is zero (the
//     cheap path the lazy commit d1dcb7eae introduced);
//   - -autopay off -measure-manual-seat-plans: the guard forces every seat's
//     extension at each priority window, so the counter reports the plans the
//     eager publisher would have offered to the manual seats.
//
// The opt-in is measurement-only. The two runs are the same game (same seed,
// same decks, same seats, no auto-pay seat), so their engine outcomes must be
// identical: the measurement run must submit no plan of its own (Planned ==
// 0), and neither run may carry a failure.
func TestOffModeManualSeatPlanMeasurement(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	decks := planMeasurementFixture()

	off, gcOff := playGame(reg, decks, 9, 14, 20000, 0, true, false, autoPay{mode: "off"})
	measured, gcMeasure := playGame(reg, decks, 9, 14, 20000, 0, true, false, autoPay{mode: "off", measureManualSeatPlans: true})

	if off != nil || measured != nil {
		t.Fatalf("fixture games must finish cleanly: off=%v measured=%v", off, measured)
	}

	// Precondition 1: both runs really posed priority decisions to manual
	// seats -- otherwise the zero/positive assertions below could hold on an
	// empty game for the wrong reason.
	if gcOff.ap.ManualSeatPriority == 0 || gcMeasure.ap.ManualSeatPriority == 0 {
		t.Fatalf("setup: manual seats saw no priority window: off=%s measured=%s", gcOff.ap, gcMeasure.ap)
	}

	// Case A -- default off stays cheap and unmeasured.
	if gcOff.ap.ManualSeatPriorityPlan != 0 {
		t.Fatalf("default off must build no plans: %s", gcOff.ap)
	}
	if gcOff.ap.Planned != 0 || gcOff.ap.Priority != 0 {
		t.Fatalf("default off must submit no plan and hold no auto-pay seat: %s", gcOff.ap)
	}

	// Case B -- the opt-in reaches priority with a REAL plan. Precondition 2:
	// at least one manual-seat priority window actually carried a plan, so
	// the counter is not simply incremented for every window.
	if gcMeasure.ap.ManualSeatPriorityPlan == 0 {
		t.Fatalf("opt-in off-mode measurement must count a real offered plan: %s", gcMeasure.ap)
	}
	if gcMeasure.ap.ManualSeatPriorityPlan > gcMeasure.ap.ManualSeatPriority {
		t.Fatalf("with-plan count exceeds the priority windows it is drawn from: %s", gcMeasure.ap)
	}

	// Measurement-only: the run is the same manual game and submits no plan.
	if gcMeasure.ap.Planned != 0 || gcMeasure.ap.Priority != 0 {
		t.Fatalf("measurement must not turn a manual seat into an auto-pay seat: %s", gcMeasure.ap)
	}
	// The measured run must reflect the real game shape, not a rebuilt one:
	// the default run's manual-seat priority windows are the same as the
	// measured run's (the flag builds extensions, it does not change choices).
	if gcOff.ap.ManualSeatPriority != gcMeasure.ap.ManualSeatPriority {
		t.Fatalf("the flag changed the game: manual-seat priorities %d vs %d", gcOff.ap.ManualSeatPriority, gcMeasure.ap.ManualSeatPriority)
	}
}
