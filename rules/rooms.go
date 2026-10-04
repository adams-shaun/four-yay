package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// rooms.go implements Enchantment Rooms (CR 309, Forge's AlternateMode:Split
// two-face enchantments -- 128 corpus files). Either Room half may be cast;
// the chosen face is unlocked from entry and the other stays locked and inert.
// As a sorcery the controller may pay the locked half's mana cost (its own
// Face.ManaCost, priced through the ordinary offerCostFor/castable gate) to
// unlock it: one DoorUnlock event, whose Apply flips the room's Unlocked flag.
// The unlock trigger (T:Mode$ UnlockDoor on the newly unlocked face, gated by
// ValidPlayer$ and ThisDoor$ True) queues off that same event, and that face's
// rules text (triggers, statics, activated abilities) becomes live.
//
// Face liveness convention: a room's live faces are its cast face (FaceIdx)
// always, plus the other face once unlocked. The room-aware scans walk both
// live faces; other readers keep using Face(). This engine does not model the
// CR-613 characteristic combination of both doors, but no supported Room half
// carries P/T.

// roomLockedFace returns the other, still locked face of a room permanent,
// or nil when the object is not a two-door room or is already unlocked.
func roomLockedFace(o *state.Object) *cards.Face {
	if o == nil || o.Unlocked || o.Card == nil || len(o.Card.Faces) != 2 || int(o.FaceIdx) >= len(o.Card.Faces) {
		return nil
	}
	locked := 1 - int(o.FaceIdx)
	if !isRoomFace(o.Card.Faces[o.FaceIdx]) || !isRoomFace(o.Card.Faces[locked]) {
		return nil
	}
	return o.Card.Faces[locked]
}

// roomAlternateCastFace reports the other castable Room half. Both halves
// must be Rooms, so generic split cards never enter through this path.
func roomAlternateCastFace(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || len(o.Card.Faces) != 2 || int(o.FaceIdx) >= len(o.Card.Faces) {
		return nil
	}
	alt := 1 - int(o.FaceIdx)
	if !isRoomFace(o.Card.Faces[o.FaceIdx]) || !isRoomFace(o.Card.Faces[alt]) {
		return nil
	}
	return o.Card.Faces[alt]
}

// isRoomFace reports whether a face is a Room half (Enchantment Room). Both
// halves of a Forge AlternateMode:Split room carry the Room type.
func isRoomFace(f *cards.Face) bool {
	return f != nil && f.IsEnchantment() && f.IsRoom()
}

// isRoom reports whether the object is a Room permanent.
func isRoom(o *state.Object) bool {
	return o != nil && o.Card != nil && len(o.Card.Faces) > 0 && o.Card.Faces[0].IsRoom()
}

// checkUnlockTriggers queues the newly unlocked face's T:Mode$ UnlockDoor
// triggers (CR 309.5's "when you unlock this door"). Only the DoorUnlock
// event's own room is scanned; after Apply, Unlocked is true and the face whose
// trigger fires is the one other than the face the Room was cast as.
func (e *Engine) checkUnlockTriggers(ev events.Event) {
	o := e.G.Obj(ev.Obj)
	if o == nil || o.Zone != state.ZBattlefield || !isRoom(o) || !o.Unlocked {
		return
	}
	if o.Card == nil || len(o.Card.Faces) != 2 || int(o.FaceIdx) >= len(o.Card.Faces) {
		return
	}
	// DoorUnlock has already set Unlocked, so the face whose trigger fires is
	// the one other than the face the Room was cast as.
	e.queueUnlockTriggers(o, o.Card.Faces[1-int(o.FaceIdx)])
}

// checkRoomEntryUnlockTriggers queues the CAST face's T:Mode$ UnlockDoor
// triggers when a Room spell enters the battlefield (CR 709.5d/709.5h): the
// door the Room was cast as is given the unlocked designation as it enters, so
// its own "when you unlock this door" ability must fire on entry. The cast
// face is the FaceIdx face; the ordinary per-face walk above cannot reach it
// because the UnlockDoor matcher is gated to the DoorUnlock event. The entry
// event itself is the MoveZone whose From is the stack -- the exact "a
// resolving Room spell enters" gate (rules/resolution.go moveResolvedOffStack).
// It fires once per entry, so it can never double-queue against the paid
// unlock's DoorUnlock path (that hook gates on DoorUnlock).
func (e *Engine) checkRoomEntryUnlockTriggers(ev events.Event) {
	if ev.Kind != events.MoveZone || ev.To != state.ZBattlefield || ev.From != state.ZStack {
		return
	}
	o := e.G.Obj(ev.Obj)
	// Unlocked is false here: it means "the alternate door has been unlocked",
	// not "the cast face is live". Only the entry designation is at issue.
	if o == nil || o.Zone != state.ZBattlefield || !isRoom(o) || o.Unlocked {
		return
	}
	e.queueUnlockTriggers(o, o.Face())
}

// queueUnlockTriggers appends one pendingTrigger per T:Mode$ UnlockDoor trigger
// on face. ValidPlayer$ (You) is the room's controller, and ThisDoor$ True
// holds by construction -- the trigger belongs to the face whose door was
// unlocked. A trigger that names no ValidPlayer$ still fires (its room is its
// owner's business); one whose ValidPlayer$ is anything but You degrades to no
// fire (fail-closed, the unhandled-qualifier convention MatchesPlayerSpec
// applies). The queue entry is the delayed-shape pendingTrigger (the ability is
// an SVar body reached through Execute$, which TriggerPush cannot carry for a
// non-primary face).
func (e *Engine) queueUnlockTriggers(o *state.Object, face *cards.Face) {
	if o == nil || face == nil || !isRoomFace(face) {
		return
	}
	for _, t := range face.Triggers {
		if t.Mode != "UnlockDoor" {
			continue
		}
		if vp := t.ParamStr(cards.PKValidPlayer); vp != "" && vp != "You" {
			continue
		}
		if td := t.ParamStr(cards.PKThisDoor); td != "" && !strings.EqualFold(td, "True") {
			continue
		}
		exec := t.ParamStr(cards.PKExecute)
		if exec == "" || t.Effect == nil {
			continue
		}
		e.pendingTriggers = append(e.pendingTriggers, pendingTrigger{
			Source:     o.ID,
			Controller: o.Controller,
			Delayed:    true,
			DelayedID:  ^uint32(0),
			Execute:    exec,
			SA:         t.Effect,
			Ctx:        effects.NewCtx(o.ID, o.Controller, effects.CtxInit{}),
		})
	}
}

// unlockMods composes the CR 601.2f cost modifiers that reach o's unlock
// special action (specialActionScope("unlock"): Inquisitive Glimmer's
// ValidSpell$ Static.Unlock). The unlock pays mana only, so a modifier that
// adds a non-mana cost part (a RaiseCost Cost$ extra) reports ok=false and
// the action is withheld rather than the part silently waived.
func (e *Engine) unlockMods(p state.PlayerID, id state.ObjID) (costMods, bool) {
	mods := e.costModifiers(p, id, specialActionScope("unlock"))
	if mods.HasExtra {
		return costMods{}, false
	}
	return mods, true
}

// unlockRoomCost returns the locked half's mana cost, parsed, for the offer
// and payment gate.
func (e *Engine) unlockRoomCost(o *state.Object) (Cost, bool) {
	f := roomLockedFace(o)
	if f == nil {
		return Cost{}, false
	}
	return e.faceCost(f), true
}
