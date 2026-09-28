package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07 revision.
// CR 118.8 (lines 1734-1739): an additional cost may involve moving objects
// between zones; CR 118.8b (line 1743): some additional costs are OPTIONAL --
// a cost the rules let the player pay must be OFFERED as payable, never
// decline-only. CR 601.2h (lines 4744-4746): the total cost is paid as the
// final step; "Partial payments are not allowed. Unpayable costs can't be
// paid" -- so a cost token whose TYPE SLOT names a whole zone (`All`) moves
// the ENTIRE zone, and an empty zone cannot pay it (the token still demands
// part.N cards). The carrier here is a TRIGGERED ABILITY's `Cost$` idiom
// (Herigast, Erupting Nullkite's cast trigger), not a cast, so 601.2h is
// cited as the 601.2 family's cost-payment anchor: whatever the engine judges
// payable must actually be offered and, when paid, move the whole zone.
//
// This is the CR-lane mirror of rules/exile_from_hand_all_cost_test.go (the
// behaviour fix's own test): the whole-zone reading of an Exile cost token
// lives nowhere else in the -run TestCR lane, so the class was invisible to
// the issue ledger for its whole life.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestCR118WholeZoneCostPaysEntireZone pins the CR 118.8 / CR 601.2h reading
// of a whole-zone cost token end to end through the real compiled corpus card
// Herigast, Erupting Nullkite (`Cost$ ExileFromHand<1/All>`): the pay/decline
// window offers the PAY option (not the decline-only list the card-filter
// misreading produced), paying exiles every card in the hand -- not one --
// and the Draw 3 body runs; an empty hand is decline-only because the token
// still demands part.N (1) cards.
func TestCR118WholeZoneCostPaysEntireZone(t *testing.T) {
	t.Parallel()

	t.Run("payable, pays the entire zone", func(t *testing.T) {
		junk := card(t, discardCostJunk)
		// Herigast + three junk: four cards in hand when the window opens
		// (the spell has already left the hand for the stack).
		e, cfg, _ := herigastHandEngine(t, junk, junk, junk)

		// Precondition: the hand holds Herigast + three junk, so the whole
		// zone is strictly more than any single-card subset a card-filter
		// misreading of `All` could ever admit.
		if got := len(e.G.Zone(state.ZHand, 0)); got != 4 {
			t.Fatalf("fixture hand = %d cards, want 4 (Herigast + 3 junk)", got)
		}

		d := castHerigastAndOpenWindow(t, e)
		pay, decline := triggerCostWindowAskDecision(t, d)
		if pay < 0 {
			t.Fatalf("CR 118.8b: the optional whole-zone Exile cost was offered DECLINE-ONLY: %+v", d.Options)
		}

		handBefore := len(e.G.Zone(state.ZHand, 0))
		if handBefore < 2 {
			t.Fatalf("precondition: hand at the window = %d cards, want >= 2 so the whole-zone reading differs from a one-card pick", handBefore)
		}
		libBefore := len(e.G.Zone(state.ZLibrary, 0))
		mark := len(e.L.Events)

		submitChoices(t, e, pay)
		passUntilStackEmpty(t, e, 40)

		// CR 118.8: paying moved the ENTIRE zone -- every hand card went to
		// exile, not a single card -- and then the body drew three, so the
		// net hand is handBefore - handBefore + 3 = 3.
		if got := len(e.G.Zone(state.ZHand, 0)); got != 3 {
			t.Fatalf("hand after pay = %d, want 3 (whole hand exiled, then 3 drawn)", got)
		}
		exiles := 0
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZExile {
				exiles++
			}
		}
		if exiles != handBefore {
			t.Fatalf("CR 118.8: exile events = %d, want %d (the WHOLE zone, not a single card)", exiles, handBefore)
		}
		draws := 0
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.Draw && ev.Player == 0 {
				draws++
			}
		}
		if draws != 3 {
			t.Fatalf("draw events = %d, want 3 (the paid body's Draw 3)", draws)
		}
		if got := libBefore - len(e.G.Zone(state.ZLibrary, 0)); got != 3 {
			t.Fatalf("library shrank by %d cards, want 3", got)
		}
		replayCheck(t, e, cfg)
		_ = decline
	})

	t.Run("not payable empty", func(t *testing.T) {
		// CR 601.2h: "Unpayable costs can't be paid" -- `All` still demands
		// part.N (1) cards, so a hand that is empty once Herigast is on the
		// stack cannot pay, and the window must be decline-only with paying
		// (declining) moving nothing.
		e, _, _ := herigastHandEngine(t)
		if got := len(e.G.Zone(state.ZHand, 0)); got != 1 {
			t.Fatalf("fixture hand = %d, want 1 (Herigast only)", got)
		}

		d := castHerigastAndOpenWindow(t, e)
		pay, decline := triggerCostWindowAskDecision(t, d)
		if pay >= 0 {
			t.Fatalf("CR 601.2h: an empty-zone ExileFromHand<1/All> cost was offered as payable: %+v", d.Options)
		}
		mark := len(e.L.Events)
		submitChoices(t, e, decline)
		passUntilStackEmpty(t, e, 40)
		for _, ev := range e.L.Events[mark:] {
			if ev.Kind == events.Draw || (ev.Kind == events.MoveZone && ev.From == state.ZHand && ev.To == state.ZExile) {
				t.Fatalf("the unpayable cost still moved a card: %+v", ev)
			}
		}
	})
}
