package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chooserSA is a minimal SA carrying one Chooser$ spelling; both choosers read
// only that parameter.
func chooserSA(spelling string) *cards.SA {
	return &cards.SA{Params: map[string]string{"Chooser": spelling}}
}

// choosePlayerOnSource records a ChoosePlayer answer on objID through the real
// Choose "chosen" event fold, exactly as DBChoosePlayer's effect does.
func choosePlayerOnSource(h *fakeHost, objID state.ObjID, p state.PlayerID) {
	h.Emit(events.Event{Kind: events.Choose, Obj: objID, Counter: "chosen",
		IDs: []state.ObjID{state.PlayerRef(p)}})
}

// TestChooserChosenPlayerResolvesTheChosenSeat pins Chooser$ ChosenPlayer (and
// its Player.Chosen spelling) on BOTH pickers: the hidden-library search
// chooser and the public-origin hidden pick chooser. Before this read,
// searchChooser fell through to c.Controller and hiddenPickChooser to the
// fetch `owner`, so an opponent asked to choose a card from the caster's own
// library/graveyard never got the ask -- Burning-Rune Demon's whole point.
//
// The two choosers share one resolver, so the bindings cannot drift: a chosen
// seat resolves identically on both, and a dead chosen seat fails closed to
// each caller's own deterministic default.
func TestChooserChosenPlayerResolvesTheChosenSeat(t *testing.T) {
	h := newHost(t, 3)
	card := mkCard(t, "Name:Fixture\nTypes:Creature\nPT:1/1\nOracle:x\n")
	src := h.g.AddObject(card, 0)
	// Precondition: no chosen player is bound yet.
	if o := h.g.Obj(src.ID); o == nil || len(o.Chosen) != 0 {
		t.Fatalf("precondition: source already carries a chosen answer: %+v", o)
	}
	c := &Ctx{Source: src.ID, Controller: 0}

	// No chosen answer: each chooser keeps its own default (search =
	// controller, hidden pick = the fetch `owner`).
	if got := searchChooser(h, c, ChangeZoneOf(chooserSA("ChosenPlayer"))); got != 0 {
		t.Fatalf("searchChooser with no chosen answer = %d, want controller 0", got)
	}
	if got := hiddenPickChooser(h, c, ChangeZoneOf(chooserSA("ChosenPlayer")), 2); got != 2 {
		t.Fatalf("hiddenPickChooser with no chosen answer = %d, want owner 2", got)
	}

	// Record seat 1 as the chosen player through the real event fold.
	choosePlayerOnSource(h, src.ID, 1)
	if o := h.g.Obj(src.ID); o == nil || len(o.Chosen) == 0 {
		t.Fatalf("precondition: Choose event did not record a chosen player: %+v", o)
	}

	// Both spellings resolve to the chosen seat on both choosers. The hidden
	// pick's `owner` is deliberately 2 (a different seat), to prove the chosen
	// answer wins over the owner fallback.
	for _, spelling := range []string{"ChosenPlayer", "Player.Chosen"} {
		if got := searchChooser(h, c, ChangeZoneOf(chooserSA(spelling))); got != 1 {
			t.Fatalf("searchChooser Chooser$ %s = %d, want chosen seat 1", spelling, got)
		}
		if got := hiddenPickChooser(h, c, ChangeZoneOf(chooserSA(spelling)), 2); got != 1 {
			t.Fatalf("hiddenPickChooser Chooser$ %s = %d, want chosen seat 1 (not owner 2)", spelling, got)
		}
	}

	// A chooser with no ChosenPlayer spelling still uses the owner fallback.
	if got := hiddenPickChooser(h, c, ChangeZoneOf(chooserSA("You")), 2); got != 0 {
		t.Fatalf("hiddenPickChooser Chooser$ You = %d, want controller 0", got)
	}
	if got := hiddenPickChooser(h, c, ChangeZoneOf(&cards.SA{}), 2); got != 2 {
		t.Fatalf("hiddenPickChooser with no Chooser$ = %d, want owner 2", got)
	}

	// The hidden-HAND mover's chooser (handMoveChooserFor) is the third
	// player-chooser sibling: it must resolve the chosen seat too, and fail
	// CLOSED (never to the hand owner) when the seat is unbound or gone.
	if got, ok := handMoveChooserFor(h, c, ChangeZoneOf(chooserSA("ChosenPlayer")), 2); !ok || got != 1 {
		t.Fatalf("handMoveChooserFor Chooser$ ChosenPlayer = (%d, %v), want chosen seat 1", got, ok)
	}
	if got, ok := handMoveChooserFor(h, c, ChangeZoneOf(&cards.SA{}), 2); !ok || got != 2 {
		t.Fatalf("handMoveChooserFor with no Chooser$ = (%d, %v), want owner fallback (2, true)", got, ok)
	}

	// A chosen seat that has left the game must not receive the ask: each
	// chooser returns its own deterministic default instead.
	h.g.Players[1].Lost = true
	if got := searchChooser(h, c, ChangeZoneOf(chooserSA("ChosenPlayer"))); got != 0 {
		t.Fatalf("searchChooser with a dead chosen seat = %d, want controller 0", got)
	}
	if got := hiddenPickChooser(h, c, ChangeZoneOf(chooserSA("ChosenPlayer")), 2); got != 2 {
		t.Fatalf("hiddenPickChooser with a dead chosen seat = %d, want owner 2", got)
	}
	if got, ok := handMoveChooserFor(h, c, ChangeZoneOf(chooserSA("ChosenPlayer")), 2); ok || got != 2 {
		t.Fatalf("handMoveChooserFor with a dead chosen seat = (%d, %v), want fail-closed (2, false)", got, ok)
	}
}
