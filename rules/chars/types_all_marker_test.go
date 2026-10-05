package chars

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

type allTypesTestBoard struct {
	game   *state.Game
	active []state.ContinuousEffect
}

func (b allTypesTestBoard) Game() *state.Game                { return b.game }
func (b allTypesTestBoard) Active() []state.ContinuousEffect { return b.active }
func (allTypesTestBoard) Matches(*state.ContinuousEffect, state.ObjID, []string, []string, state.Zone, PTBind) bool {
	return true
}
func (allTypesTestBoard) StaticAmount(*state.ContinuousEffect, string, state.ObjID) int32 { return 0 }
func (allTypesTestBoard) ControllerOf(state.ObjID) state.PlayerID                         { return 0 }
func (allTypesTestBoard) LandTypeWords() []string                                         { return nil }
func (allTypesTestBoard) CDAContext(*state.Object, *cards.Face) *effects.Ctx              { return nil }
func (allTypesTestBoard) Host() effects.Host                                              { return nil }

func TestTypesAllCreatureMarkerClearedByLaterSubtypeRemoval(t *testing.T) {
	card := &cards.Card{Faces: []*cards.Face{{Name: "Test Bear", Types: []string{"Creature", "Bear"}}}}
	game := state.NewGame([]string{"A"})
	obj := game.AddObject(card, 0)
	id := obj.ID
	board := allTypesTestBoard{
		game: game,
		active: []state.ContinuousEffect{
			{Layer: state.LType, AddAllCreatureTypes: true},
			{Layer: state.LType, RemoveTypes: []string{"Bear"}},
		},
	}

	if !containsType(obj.Face().Types, "Bear") {
		t.Fatal("precondition: test permanent does not start with Bear subtype")
	}
	types, all := TypesAndAllCreatureTypes(board, board.active, id, 0)
	if containsType(types, "Bear") {
		// The later effect must actually remove the subtype; this guards
		// against a vacuous effect setup.
		t.Fatalf("precondition: later type effect did not remove Bear: %v", types)
	}
	if all {
		t.Fatalf("later subtype removal left all-creature-types marker set: %v", types)
	}
}

func containsType(types []string, want string) bool {
	for _, typ := range types {
		if typ == want {
			return true
		}
	}
	return false
}
