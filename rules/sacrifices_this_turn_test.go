package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestSacrificedThisTurnCountsTheSacrificer: CR 701.21a -- only a
// permanent's controller can sacrifice it, so the sacrificer is the
// controller at the instant of the move. events.Apply's MoveZone fold stamps
// that controller onto the zone-entry record (state.ZoneEntry.Sacrificer)
// BEFORE the CR 400.7 reset returns the card to its owner, which is the
// actor provenance the PlayerCount*$SacrificedThisTurn heads read.
func TestSacrificedThisTurnCountsTheSacrificer(t *testing.T) {
	t.Parallel()
	e := New(Config{Seed: 1, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40)}})
	creature := card(t, "Name:Creature\nTypes:Creature\nPT:2/2\nOracle:x\n")
	permanent := e.G.AddObject(creature, 1)
	permanent.Controller = 0
	permanent.Zone = state.ZBattlefield
	if permanent.Owner != 1 || permanent.Controller != 0 || permanent.Zone != state.ZBattlefield {
		t.Fatalf("precondition: need a battlefield permanent owned by player 1 and controlled by player 0: %+v", permanent)
	}

	// Player 0 sacrifices a permanent owned by player 1: the count is player
	// 0's, never the owner's.
	e.emit(events.Sacrifice(permanent.ID))
	if !events.IsSacrifice(e.L.Events[len(e.L.Events)-1]) {
		t.Fatal("precondition: expected a canonical sacrifice event")
	}
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, "PlayerCountPropertyYou$SacrificedThisTurn Creature"); !ok || got != 1 {
		t.Fatalf("player 0 SacrificedThisTurn = (%d, %v), want (1, true): player 0 controlled it", got, ok)
	}
	if got, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 1}, "PlayerCountPropertyYou$SacrificedThisTurn Creature"); !ok || got != 0 {
		t.Fatalf("player 1 SacrificedThisTurn = (%d, %v), want (0, true): the owner did not sacrifice it", got, ok)
	}
	if got, _ := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, "PlayerCountPropertyYou$SacrificedThisTurn Artifact"); got != 0 {
		t.Fatalf("player 0 SacrificedThisTurn Artifact = %d, want 0 (the spec filters)", got)
	}
}
