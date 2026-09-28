package events

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonCompleteRequiresActiveCommandZoneObject(t *testing.T) {
	g := state.NewGame([]string{"Ann"})
	// The zero ObjID must not compare equal to an absent active dungeon.
	Apply(g, Event{Kind: DungeonComplete, Player: 0, Obj: 0})
	if got := g.Players[0].CompletedDungeons; got != 0 {
		t.Fatalf("completion with no dungeon: %d", got)
	}
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nTypes:Dungeon\nK:Dungeon:DBEntrance\nSVar:DBEntrance:DB$ Scry | RoomName$ Cave Entrance\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon: %v", diags)
	}
	c.Link()
	g.Tokens = map[string]*cards.Card{"mine": c}
	Apply(g, Event{Kind: DungeonCreate, Player: 0, Text: "mine"})
	id := g.Players[0].DungeonObj
	if id == 0 || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("precondition: no command-zone dungeon: id=%d", id)
	}
	Apply(g, Event{Kind: DungeonComplete, Player: 0, Obj: 0})
	if g.Players[0].CompletedDungeons != 0 {
		t.Fatal("zero object completed active dungeon")
	}
	Apply(g, Event{Kind: DungeonComplete, Player: 0, Obj: id})
	if g.Players[0].CompletedDungeons != 1 {
		t.Fatal("valid command-zone completion was not counted")
	}
	Apply(g, Event{Kind: DungeonRemove, Player: 0, Obj: id})
	Apply(g, Event{Kind: DungeonComplete, Player: 0, Obj: id})
	if g.Players[0].CompletedDungeons != 1 {
		t.Fatal("removed dungeon counted again")
	}
}
