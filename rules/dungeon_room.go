package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The dungeon chain's slice 3: CR 309.4c's room abilities and CR 704.5t's
// dungeon-completion state-based action.
//
// A dungeon is a token-script object in a player's command zone (slice 1),
// and each room is one SVar on that script's face (`DB$ <effect> |
// RoomName$ ... | NextRoom$ A,B`, Forge's representation). Neither the
// command zone nor a token script's SVar table is walked by the ordinary
// per-face trigger scan, so both halves are synthetic scans off the
// lifecycle event, the checkUnlockTriggers / checkRingEmblemTriggers
// precedent:
//
//   - checkDungeonRoomTriggers queues the room's ability when the marker
//     moves into it. The queue entry is the delayed shape (DelayedID -1 =
//     "no registration to remove", Execute the room SVar name) exactly as a
//     Saga chapter or an unlocked Room's door uses it: the ability object is
//     minted inside events.Apply so a log-only replay rebuilds it, it goes
//     through the ordinary CR 603.3c target placement (Storeroom's
//     PutCounter, Fungi Cavern's Pump and Trap's LoseLife all target), and
//     it resolves on the stack normally. Its Source is the dungeon object in
//     the command zone, so CR 704.5t's "isn't the source of a room ability
//     that has triggered but not yet left the stack" reads the same
//     pending-queue + stack scan checkSagas uses.
//
//   - dungeonCompletion is that state-based action: a dungeon whose marker
//     is on a bottommost room (no NextRoom$ arrow) and which is the source of
//     no pending or stacked room ability is completed. Completion is the two
//     explicit transitions slice 1 defined -- the count increment, then the
//     token leaving the command zone -- so replay derives both.

// checkDungeonRoomTriggers queues the room ability of the room the marker
// just entered (CR 309.4c). The event's Text is the room key, which is also
// the room SVar's name on the dungeon script's face. A room that resolves no
// SVar (a hand-built DungeonRoom naming an unknown key) queues nothing --
// fail closed, the same direction the venture walk's unknown-key arrow takes.
func (e *Engine) checkDungeonRoomTriggers(ev events.Event) {
	if ev.Text == "" {
		return
	}
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone != state.ZCommand || o.Face() == nil {
		return
	}
	// CR 309.4c: each room ability is controlled by the player who owns the
	// dungeon card that is the ability's source. Guard a departed owner the
	// same way every other trigger queueing site does (CR 800.4a).
	if int(o.Owner) >= len(e.G.Players) || e.G.Players[o.Owner].Lost {
		return
	}
	sa := cards.ResolveSVar(o.Face().SVars, ev.Text)
	if sa == nil {
		return
	}
	e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
		Source:     ev.Obj,
		Controller: o.Owner,
		Delayed:    true,
		DelayedID:  ^uint32(0),
		Execute:    ev.Text,
		SA:         sa,
		Ctx: effects.Ctx{
			Source:     ev.Obj,
			Controller: o.Owner,
		},
	})
}

// dungeonCompletion is CR 704.5t: if a player's venture marker is on the
// bottommost room of a dungeon card, and that dungeon card isn't the source
// of a room ability that has triggered but not yet left the stack, the
// dungeon card's owner removes it from the game -- a completion (CR 309.7).
//
// "Left the stack" is checked as "no pending trigger and no stack object has
// this dungeon as its Source", the exact scan checkSagas uses for a Saga's
// final chapter. The room ability is minted with Source = the dungeon, so
// the scan covers both the queue and the stack. A busy dungeon defers and
// marks sbaUnquiet (a runtime input the quiet key does not cover, the
// checkSagas precedent). One attempt per dungeon per checkStateBased call
// (tried.dungeons), re-armed on an alive-set shrink like every other pass.
//
// Completion is emitted as the two slice-1 transitions in order: the count
// increment (DungeonComplete, latched once by DungeonCompleted) and then the
// token leaving the command zone (DungeonRemove). Both are needed: the
// count must survive a replayed log even though the object is gone.
func (e *Engine) dungeonCompletion(tried *sbaAttempts) bool {
	changed := false
	for p := range e.G.Players {
		pl := state.PlayerID(p)
		id := e.G.Players[p].DungeonObj
		if id == 0 || e.G.Players[p].Lost {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZCommand || o.Face() == nil {
			continue
		}
		room := e.G.Players[p].DungeonRoom
		if room == "" {
			continue
		}
		// Bottommost iff the room prints no NextRoom$ arrow. An unresolvable
		// room key also answers empty; it is treated the same way the venture
		// walk treats it (nowhere to go), so completion cannot strand a
		// marker on a room the script does not define.
		if len(effects.DungeonNextRooms(o.Face(), room)) > 0 {
			continue
		}
		busy := false
		for i := range e.pendingTriggers {
			if e.pendingTriggers[i].Source == id {
				busy = true
				break
			}
		}
		if !busy {
			for _, sid := range e.G.Stack {
				if so := e.G.Obj(sid); so != nil && so.Source == id {
					busy = true
					break
				}
			}
		}
		if busy {
			if !tried.dungeons[id] {
				e.sbaUnquiet = true
			}
			continue
		}
		if tried.dungeons[id] {
			continue
		}
		tried.dungeons[id] = true
		if !e.G.Players[p].DungeonCompleted {
			e.emit(events.Event{Kind: events.DungeonComplete, Player: pl, Obj: id})
		}
		e.emit(events.Event{Kind: events.DungeonRemove, Player: pl, Obj: id})
		changed = true
	}
	return changed
}
