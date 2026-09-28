package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonCompleteHelperDoesNotEmitDuplicate(t *testing.T) {
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nTypes:Dungeon\nK:Dungeon:Entrance\nSVar:Entrance:DB$ Scry | RoomName$ Cave Entrance\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon: %v", diags)
	}
	c.Link()
	e := New(Config{Seed: 13, Names: []string{"Ann", "Bob"}, Tokens: map[string]*cards.Card{"mine": c}})
	id := e.PutDungeon(0, "mine")
	if id == 0 || e.G.Obj(id).Zone != state.ZCommand {
		t.Fatalf("precondition: dungeon not active: %d", id)
	}
	e.CompleteDungeon(0)
	if e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("first completion: %d", e.G.Players[0].CompletedDungeons)
	}
	before := len(e.L.Events)
	e.CompleteDungeon(0)
	if len(e.L.Events) != before || e.G.Players[0].CompletedDungeons != 1 {
		t.Fatalf("duplicate helper completion: events=%d want=%d count=%d", len(e.L.Events), before, e.G.Players[0].CompletedDungeons)
	}
	completions := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DungeonComplete {
			completions++
		}
	}
	if completions != 1 {
		t.Fatalf("precondition: expected one completion event, got %d", completions)
	}
}
