package events

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonEventsReplayState(t *testing.T) {
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nManaCost:no cost\nTypes:Dungeon\nK:Dungeon:Entrance,Temple\nSVar:Entrance:DB$ Scry | RoomName$ Cave Entrance | NextRoom$ Temple\nSVar:Temple:DB$ Draw | RoomName$ Temple of Dumathoin\nOracle:dungeon\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon fixture: %v", diags)
	}
	c.Link()
	g := state.NewGame([]string{"Ann", "Bob"})
	g.Tokens = map[string]*cards.Card{"lost_mine_of_phandelver": c}
	log := NewLog(7)
	Emit(g, log, Event{Kind: DungeonCreate, Player: 0, Text: "lost_mine_of_phandelver"})
	id := g.Players[0].DungeonObj
	if id == 0 || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("created dungeon object = %+v, want command-zone object", g.Obj(id))
	}
	Emit(g, log, Event{Kind: DungeonRoom, Player: 0, Obj: id, Text: "Entrance"})
	Emit(g, log, Event{Kind: DungeonRoom, Player: 0, Obj: id, Text: "Temple"})
	if g.Players[0].DungeonRoom != "Temple" || g.Obj(id).Zone != state.ZCommand {
		t.Fatalf("after advancing: room=%q zone=%v", g.Players[0].DungeonRoom, g.Obj(id).Zone)
	}
	Emit(g, log, Event{Kind: DungeonComplete, Player: 0, Obj: id})
	Emit(g, log, Event{Kind: DungeonRemove, Player: 0, Obj: id})
	if g.Players[0].CompletedDungeons != 1 || g.Players[0].DungeonObj != 0 || g.Obj(id).Zone != state.ZCeased {
		t.Fatalf("completed state: count=%d active=%d zone=%v", g.Players[0].CompletedDungeons, g.Players[0].DungeonObj, g.Obj(id).Zone)
	}

	rebuilt := state.NewGame([]string{"Ann", "Bob"})
	rebuilt.Tokens = g.Tokens
	rebuiltLog := NewLog(7)
	for _, ev := range log.Events {
		stored := rebuiltLog.Append(ev)
		Apply(rebuilt, stored)
	}
	if rebuiltLog.Head() != log.Head() || rebuilt.Players[0].CompletedDungeons != 1 || rebuilt.NextID != g.NextID || rebuilt.Obj(id).Zone != state.ZCeased {
		t.Fatalf("replay differs: head=%s want=%s player=%+v obj=%+v", rebuiltLog.Head(), log.Head(), rebuilt.Players[0], rebuilt.Obj(id))
	}
}
