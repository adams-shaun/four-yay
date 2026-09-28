package effects

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() { Register("EndTurn", effEndTurn) }

// effEndTurn implements DB$/SP$/AB$ EndTurn (Forge's EndTurnEffect; 9 corpus
// carriers: Time Stop, Discontinuity, Glorious End, Sundial of the Infinite,
// Day's Undoing, Ultima, Hurkyl's Final Meditation, Obeka Brute Chronologist
// and The Drum, Mining Facility). CR 723.1's "end the turn" is a
// resolution-time turn-structure change, so like DB$ AddPhase it goes through
// events (AGENTS.md's single-mutation-point rule) rather than writing state
// here: one events.EndTurn whose IDs is a snapshot of the whole stack taken
// BEFORE the emit. events.Apply's EndTurn case is the fold -- it exiles every
// named stack object (the resolving spell included, so Time Stop is exiled
// rather than put in the graveyard) and removes every creature/planeswalker
// from combat (CR 723.1a/c). The rules engine observes the EndTurn event,
// clears the triggers still waiting to be placed and jumps the turn straight
// to the cleanup step (CR 723.1d/e -- see Engine.finishEndTurn).
//
// The stack is snapshotted in the effect, not read from the fold, because by
// the time events.Apply runs the resolving object is still on the stack and
// the fold must move exactly the objects the turn's end is replacing:
// resolving after a MoveZone (or after an intervening replacement) would exile
// a different set.
//
// Known deviation (reported, not closed here): Obeka's ability carries
// `Defined$ ActivePlayer | Optional$ True` ("the player whose turn it is MAY
// end the turn") and this effect ends the turn unconditionally, ignoring both
// parameters. The other carriers' parameters are handled elsewhere
// (ConditionPlayerTurn$ by the shared condition gate in conditions.go;
// Sundial's PlayerTurn$ True is an activation restriction outside this
// primitive's scope).
func effEndTurn(h Host, _ *Ctx, _ *cards.SA) {
	g := h.Game()
	ids := append([]state.ObjID(nil), g.Stack...)
	h.Emit(events.Event{Kind: events.EndTurn, IDs: ids})
}
