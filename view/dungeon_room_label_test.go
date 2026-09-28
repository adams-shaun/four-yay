package view

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestDungeonRoomLabelUsesRoomName(t *testing.T) {
	// Real Forge shape: the key is DBEntrance, but the public name is Cave Entrance.
	c, diags := cards.ParseBytes("dungeon.txt", []byte("Name:Lost Mine of Phandelver\nTypes:Dungeon\nK:Dungeon:DBEntrance,DBTempleDumathoin\nSVar:DBEntrance:DB$ Scry | ScryNum$ 1 | RoomName$ Cave Entrance | NextRoom$ DBTempleDumathoin\nSVar:DBTempleDumathoin:DB$ Draw | RoomName$ Temple of Dumathoin\n"))
	if len(diags) != 0 {
		t.Fatalf("parse dungeon: %v", diags)
	}
	c.Link()
	g := state.NewGame([]string{"Ann", "Bob"})
	g.Tokens = map[string]*cards.Card{"mine": c}
	events.Apply(g, events.Event{Kind: events.DungeonCreate, Player: 0, Text: "mine"})
	id := g.Players[0].DungeonObj
	if id == 0 || g.Obj(id).Zone != state.ZCommand || c.Faces[0].SVars["DBEntrance"] == "" {
		t.Fatalf("precondition: script or command-zone dungeon missing: id=%d", id)
	}
	for _, step := range []struct{ key, name string }{
		{"DBEntrance", "Cave Entrance"},
		{"DBTempleDumathoin", "Temple of Dumathoin"},
	} {
		events.Apply(g, events.Event{Kind: events.DungeonRoom, Player: 0, Obj: id, Text: step.key})
		if g.Players[0].DungeonRoom != step.key {
			t.Fatalf("marker did not move to %q", step.key)
		}
		for _, viewer := range []state.PlayerID{0, 1} {
			got := Project(g, nil, viewer, nil).Players[0].Dungeon
			if got == nil || got.Name != "Lost Mine of Phandelver" || got.Room != step.name {
				t.Fatalf("viewer %d: dungeon=%+v, want room %q", viewer, got, step.name)
			}
		}
	}
}
