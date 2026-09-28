package events

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonCompleteOnlyOncePerActiveObject(t *testing.T) {
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nTypes:Dungeon\nK:Dungeon:Entrance\nSVar:Entrance:DB$ Scry | RoomName$ Cave Entrance\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon: %v", diags)
	}
	c.Link()
	g := state.NewGame([]string{"Ann"})
	g.Tokens = map[string]*cards.Card{"mine": c}
	Apply(g, Event{Kind: DungeonCreate, Player: 0, Text: "mine"})
	id := g.Players[0].DungeonObj
	if id == 0 || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("precondition: dungeon not in command zone: %d", id)
	}
	complete := Event{Kind: DungeonComplete, Player: 0, Obj: id}
	Apply(g, complete)
	if g.Players[0].CompletedDungeons != 1 {
		t.Fatalf("first completion: %d", g.Players[0].CompletedDungeons)
	}
	Apply(g, complete)
	if g.Players[0].CompletedDungeons != 1 || g.Players[0].DungeonObj != id || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("duplicate completion counted or removed active object: %+v", g.Players[0])
	}
	Apply(g, Event{Kind: DungeonRemove, Player: 0, Obj: id})
	Apply(g, Event{Kind: DungeonCreate, Player: 0, Text: "mine"})
	newID := g.Players[0].DungeonObj
	if newID == 0 || newID == id || g.Obj(newID).Zone != state.ZCommand {
		t.Fatalf("precondition: fresh dungeon missing: %d", newID)
	}
	Apply(g, Event{Kind: DungeonComplete, Player: 0, Obj: newID})
	if g.Players[0].CompletedDungeons != 2 {
		t.Fatalf("new dungeon not completed: %d", g.Players[0].CompletedDungeons)
	}
}
