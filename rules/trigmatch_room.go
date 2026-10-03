package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// trigmatch_room.go implements Mode$ FullyUnlock, the "Eerie -- whenever
// ... you fully unlock a Room" half of the Room mechanic (CR 709.5; 17 corpus
// files carry the line: the Eerie enchantments, e.g. Fear of Sleep
// Paralysis). It is the OTHER-permanent sibling of Mode$ UnlockDoor
// (rules/rooms.go's checkUnlockTriggers), which is the unlocked door's OWN
// "when you unlock this door" trigger and is queued by a dedicated scan of
// the unlocked face:
//
//   - UnlockDoor lives on the Room's alternate face (the door just unlocked),
//     which the ordinary per-face walk cannot reach before the flag flips, so
//     it has a dedicated queue path.
//   - FullyUnlock lives on an unrelated permanent already on the battlefield
//     (the Eerie enchantment), so the ordinary per-face walk DOES visit it.
//     Registering the matcher is therefore all that is needed: the shared
//     ValidCard$/ValidPlayer$/TriggerZones$/Phase$ gates run before it, and
//     the ordinary TriggerPush mint carries the Execute$ body.
//
// Both modes fire on the ONE DoorUnlock event the unlock activation emits
// (rules/legal.go's "unlock" arm); no extra event kind exists, and the
// state delta stays the single Unlocked flip events.Apply performs.
//
// The two-door model: a Room enters with its cast face's door unlocked and
// exactly one locked door. Unlocking that last door IS fully unlocking the
// room, so every DoorUnlock a game action can emit is a full unlock. The
// matcher additionally requires the event to be a real locked->unlocked
// transition (the pre-fold Unlocked flag rides the event's LKI snapshot),
// so a directly-emitted repeated DoorUnlock on an already-unlocked room --
// which no game action produces -- fires nothing. That keeps "fully unlock"
// honest without inventing a second event kind or a multi-door counter.
func (e *Engine) fullyUnlockMatches(t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.DoorUnlock {
		return false
	}
	room := e.G.Obj(ev.Obj)
	if room == nil || room.Zone != state.ZBattlefield || !isRoom(room) || !room.Unlocked {
		return false
	}
	// Transition gate: the room must have been locked immediately before the
	// event. lki is the pre-Apply snapshot emit captured for DoorUnlock; a
	// missing or already-unlocked snapshot is not a full unlock.
	if lki == nil || lki.ID != ev.Obj || lki.Unlocked {
		return false
	}
	you := e.controllerOf(source)
	// ValidPlayer$ names who fully unlocked the Room. The unlock activation is
	// the Room controller's, so the unlocking player is room.Controller. An
	// absent clause fires for any unlocker; a non-"You" value fails closed
	// through the ordinary spec matcher.
	if vp := t.ParamStr(cards.PKValidPlayer); vp != "" && !effects.MatchesPlayerSpec(e.G, vp, room.Controller, you) {
		return false
	}
	// ValidCard$ names the Room that was unlocked. Every corpus FullyUnlock
	// carrier says Card.Room; an absent clause defaults to that same filter.
	spec := t.ParamStr(cards.PKValidCard)
	if spec == "" {
		spec = "Card.Room"
	}
	if !e.matchesSpec(spec, ev.Obj, e.specCtx(source, you)) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(func(e *Engine, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return e.fullyUnlockMatches(t, source, ev, lki)
	}, "FullyUnlock")
	effects.RegisterNonAPI("trig:FullyUnlock")
}
