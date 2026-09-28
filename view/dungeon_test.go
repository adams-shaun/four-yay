package view

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonViewIsPublicAndTracksRoom(t *testing.T) {
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nManaCost:no cost\nTypes:Dungeon\nK:Dungeon:Entrance,Temple\nOracle:dungeon\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon fixture: %v", diags)
	}
	c.Link()
	g := state.NewGame([]string{"Ann", "Bob"})
	g.Tokens = map[string]*cards.Card{"lost_mine_of_phandelver": c}
	var id state.ObjID
	check := func(room string, completed int32) {
		t.Helper()
		v := Project(g, nil, 1, nil)
		got := v.Players[0].Dungeon
		if got == nil || got.Name != "Lost Mine of Phandelver" || got.Room != room || v.Players[0].CompletedDungeons != completed {
			t.Fatalf("opponent dungeon projection = %+v completed=%d, want room %q count %d", got, v.Players[0].CompletedDungeons, room, completed)
		}
		if len(v.Players[0].Command) != 1 || v.Players[0].Command[0].ID != id {
			t.Fatalf("dungeon missing from public command zone: %+v", v.Players[0].Command)
		}
	}
	events.Apply(g, events.Event{Kind: events.DungeonCreate, Player: 0, Text: "lost_mine_of_phandelver"})
	id = g.Players[0].DungeonObj
	if id == 0 || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("DungeonCreate did not put the object in command zone: %+v", g.Obj(id))
	}
	check("", 0)
	events.Apply(g, events.Event{Kind: events.DungeonRoom, Player: 0, Obj: id, Text: "Entrance"})
	check("Entrance", 0)
	events.Apply(g, events.Event{Kind: events.DungeonRoom, Player: 0, Obj: id, Text: "Temple"})
	check("Temple", 0)
	events.Apply(g, events.Event{Kind: events.DungeonComplete, Player: 0, Obj: id})
	check("Temple", 1)
	events.Apply(g, events.Event{Kind: events.DungeonRemove, Player: 0, Obj: id})
	if g.Players[0].DungeonObj != 0 || g.Obj(id).Zone != state.ZCeased || g.Players[0].CompletedDungeons != 1 {
		t.Fatalf("DungeonRemove state: player=%+v object=%+v", g.Players[0], g.Obj(id))
	}
}
