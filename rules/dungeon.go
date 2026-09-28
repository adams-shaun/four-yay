package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// PutDungeon creates the named token-script dungeon in the player's command
// zone. The event fold allocates the object so replay derives its identity.
func (e *Engine) PutDungeon(player state.PlayerID, tokenKey string) state.ObjID {
	if player < 0 || int(player) >= len(e.G.Players) || e.G.Players[player].DungeonObj != 0 || e.G.Tokens[tokenKey] == nil {
		return 0
	}
	e.emit(events.Event{Kind: events.DungeonCreate, Player: player, Text: tokenKey})
	return e.G.Players[player].DungeonObj
}

// MoveDungeon advances the player's venture marker to a room key from the
// active dungeon's K:Dungeon room list.
func (e *Engine) MoveDungeon(player state.PlayerID, room string) {
	if player < 0 || int(player) >= len(e.G.Players) || room == "" {
		return
	}
	id := e.G.Players[player].DungeonObj
	if id == 0 {
		return
	}
	e.emit(events.Event{Kind: events.DungeonRoom, Player: player, Obj: id, Text: room})
}

// CompleteDungeon records one completed dungeon while it remains in the
// command zone; RemoveDungeon is a separate event so the transition is
// explicit in the replayed log.
func (e *Engine) CompleteDungeon(player state.PlayerID) {
	if player < 0 || int(player) >= len(e.G.Players) {
		return
	}
	id := e.G.Players[player].DungeonObj
	if id != 0 {
		e.emit(events.Event{Kind: events.DungeonComplete, Player: player, Obj: id})
	}
}

// RemoveDungeon removes the active dungeon token from the command zone.
func (e *Engine) RemoveDungeon(player state.PlayerID) {
	if player < 0 || int(player) >= len(e.G.Players) {
		return
	}
	id := e.G.Players[player].DungeonObj
	if id != 0 {
		e.emit(events.Event{Kind: events.DungeonRemove, Player: player, Obj: id})
	}
}
