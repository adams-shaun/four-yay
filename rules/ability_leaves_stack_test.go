package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestResolvedAbilityIsNotAnExiledCard pins the cardfuzz nil-Face panics in
// delveAsk and exAsk. A resolved ability's retirement move is logged as
// stack->exile, and the Face-less ability object used to JOIN its owner's
// exile list: a later "put a card an opponent owns from exile into that
// player's graveyard" cost (Oracle of Dust) then moved it into a graveyard,
// where Delve / an ExileFromGrave cost offered it as a card and dereferenced
// its nil Face. CR 113.7a: an ability that leaves the stack ceases to exist,
// so it must never be a member of any zone's card list.
func TestResolvedAbilityIsNotAnExiledCard(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	e.G.Step = state.StepEnd
	e.finishEnteredStep()
	if e.putTriggersOnStack() || len(e.G.Stack) != 1 {
		t.Fatalf("precondition: monarch draw trigger not on the stack: %v", e.G.Stack)
	}
	ab := e.G.Stack[0]
	if o := e.G.Obj(ab); o == nil || o.Card != nil || o.Ability == nil {
		t.Fatalf("precondition: stack object %d is not a Face-less ability object", ab)
	}
	e.resolveTop()
	if o := e.G.Obj(ab); o.Zone == state.ZStack {
		t.Fatal("precondition: the ability did not leave the stack")
	}
	for p := range e.G.Players {
		for _, z := range []state.Zone{state.ZExile, state.ZGraveyard, state.ZHand, state.ZLibrary, state.ZBattlefield} {
			for _, id := range e.G.Zone(z, state.PlayerID(p)) {
				if id == ab {
					t.Fatalf("resolved ability %d is listed in seat %d's %s", ab, p, z)
				}
			}
		}
	}
}
