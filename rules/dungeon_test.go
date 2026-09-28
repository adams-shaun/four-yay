package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonHelpersEmitReplayableTransitions(t *testing.T) {
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nManaCost:no cost\nTypes:Dungeon\nK:Dungeon:Entrance,Temple\nOracle:dungeon\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon fixture: %v", diags)
	}
	c.Link()
	const key = "lost_mine_of_phandelver"
	e := New(Config{Seed: 11, Names: []string{"Ann", "Bob"}, Tokens: map[string]*cards.Card{key: c}})
	id := e.PutDungeon(0, key)
	if id == 0 || e.G.Obj(id).Zone != state.ZCommand {
		t.Fatalf("PutDungeon object id=%d object=%+v", id, e.G.Obj(id))
	}
	e.MoveDungeon(0, "Entrance")
	e.MoveDungeon(0, "Temple")
	e.CompleteDungeon(0)
	if e.G.Players[0].DungeonRoom != "Temple" || e.G.Players[0].CompletedDungeons != 1 || e.G.Obj(id).Zone != state.ZCommand {
		t.Fatalf("before remove: player=%+v object=%+v", e.G.Players[0], e.G.Obj(id))
	}
	e.RemoveDungeon(0)
	if e.G.Players[0].DungeonObj != 0 || e.G.Obj(id).Zone != state.ZCeased {
		t.Fatalf("after remove: player=%+v object=%+v", e.G.Players[0], e.G.Obj(id))
	}
	for _, kind := range []string{"dungeon_create", "dungeon_room", "dungeon_complete", "dungeon_remove"} {
		found := false
		for _, ev := range e.L.Events {
			if ev.Kind.String() == kind {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("helper did not emit %s event", kind)
		}
	}
}
