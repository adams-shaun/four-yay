package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// crimsonPulseCase is Case of the Crimson Pulse's solve and Solved lines
// (MKM); its ETB loot is dropped so putting it on the board asks nothing.
const crimsonPulseCase = `Name:Case of the Crimson Pulse
ManaCost:2 R
Types:Enchantment Case
T:Mode$ Phase | Phase$ End of Turn | ValidPlayer$ You | Hellbent$ True | IsPresent$ Card.Self+!IsSolved | TriggerZones$ Battlefield | Execute$ TrigSolve | TriggerDescription$ To solve — You have no cards in hand.
SVar:TrigSolve:DB$ AlterAttribute | Defined$ Self | Attributes$ Solved
T:Mode$ Phase | Phase$ Upkeep | IsPresent$ Card.Self+IsSolved | ValidPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDiscard | TriggerDescription$ Solved — At the beginning of your upkeep, discard your hand, then draw two cards.
SVar:TrigDiscard:DB$ Discard | Mode$ Hand | Defined$ You | SubAbility$ DBDraw
SVar:DBDraw:DB$ Draw | Defined$ You | NumCards$ 2
Oracle:To solve — You have no cards in hand.\nSolved — At the beginning of your upkeep, discard your hand, then draw two cards.
`

// TestTriggerHellbentInterveningIf pins CR 603.4 for a trigger's Hellbent$
// clause: Case of the Crimson Pulse's "To solve — You have no cards in hand"
// queues nothing at its controller's end step while a card is in hand, and
// once the hand is empty it triggers and solves the Case. A card that
// re-enters the hand before resolution makes it do nothing.
func TestTriggerHellbentInterveningIf(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	}
	cs := onBoardCard(t, e, 0, card(t, crimsonPulseCase))
	held := handCard(e, card(t, "Name:Wastes\nTypes:Basic Land\nOracle:\n"), 0)
	if n := len(e.G.Zone(state.ZHand, 0)); n != 1 {
		t.Fatalf("precondition: p0 holds %d cards, want 1", n)
	}

	if n := endStepTriggers(e); n != 0 {
		t.Fatalf("card in hand: the solve trigger queued (%d triggers)", n)
	}
	if e.G.Obj(cs).Solved {
		t.Fatal("card in hand: the Case is solved")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: held, From: state.ZHand, To: state.ZGraveyard})
	if n := len(e.G.Zone(state.ZHand, 0)); n != 0 {
		t.Fatalf("precondition: p0 still holds %d cards", n)
	}
	if n := endStepTriggers(e); n != 1 {
		t.Fatalf("empty hand: queued %d triggers, want the solve trigger", n)
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if !e.G.Obj(cs).Solved {
		t.Fatal("empty hand: the Case is not solved after its solve trigger resolved")
	}

	// Resolution re-check: a second, fresh Case triggers with an empty hand,
	// then a card arrives before it resolves; the ability does nothing.
	cs2 := onBoardCard(t, e, 0, card(t, crimsonPulseCase))
	if n := endStepTriggers(e); n != 1 {
		t.Fatalf("second Case, empty hand: queued %d triggers, want 1", n)
	}
	e.putTriggersOnStack()
	handCard(e, card(t, "Name:Wastes\nTypes:Basic Land\nOracle:\n"), 0)
	e.resolveTop()
	if e.G.Obj(cs2).Solved {
		t.Fatal("a card entered the hand before resolution but the Case still solved")
	}
}
