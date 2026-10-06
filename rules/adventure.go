package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// adventure.go implements the Adventure spell face (CR 714, Forge's
// AlternateMode:Adventure -- 151 corpus files: a permanent front and an
// Instant/Sorcery "Adventure" back). Two offers ride machinery that already
// exists for other shapes:
//
//   - from the hand, the Adventure spell face is offered as Mode
//     "adventure_alt" (the Room alternate-cast shape, rules/legal.go's hand
//     walk), gated on the ADVENTURE face's own timing, targets and cost;
//     beginCast flips the card to the spell face, and the resolution exiles
//     it into the adventure zone via spellRestZone's FlagAdventure branch,
//     where it is its main face again (CR 715.4: events.Apply's MoveZone
//     fold resets an Adventure card's face on every stack exit).
//   - from exile -- the adventure zone, where the resolving Adventure spell
//     rests -- the main face is offered as Mode "adventure_recast" (the Warp
//     recast shape, rules/legal.go's exile walk), gated on a log-derived
//     adventure-zone provenance check.
//
// The provenance check is log-derived because CastFlags reset on every
// stack->non-battlefield move (events/apply.go), which is exactly the move a
// resolving Adventure spell makes -- no object flag can carry "this card
// sits in the adventure zone". The walk mirrors warpRecastAvailable's
// fixed-order backwards scan without warp's later-turn requirement: CR
// 714.3a permits casting the main face from the adventure zone immediately.

// isAdventureSpellFace reports whether a face is an Adventure spell half:
// an Instant or Sorcery carrying the Adventure type.
func isAdventureSpellFace(f *cards.Face) bool {
	if f == nil || (!f.IsInstant() && !f.IsSorcery()) {
		return false
	}
	for _, t := range f.Types {
		if t == "Adventure" {
			return true
		}
	}
	return false
}

// adventureSpellFace returns the Adventure spell face (face 1 -- ALTERNATE
// starts face 1 in cards/parse.go) of a card whose AlternateMode is
// Adventure, or nil when the object is not a well-formed Adventure card.
func adventureSpellFace(o *state.Object) *cards.Face {
	if o == nil || o.Card == nil || o.Card.AlternateMode != "Adventure" || len(o.Card.Faces) != 2 {
		return nil
	}
	if !isAdventureSpellFace(o.Card.Faces[1]) {
		return nil
	}
	return o.Card.Faces[1]
}

// adventureZoneAvailable reports whether id -- which the caller has
// established is in exile (as its main face, CR 715.4) -- got there by
// RESOLVING an adventure_alt cast. That is the only adventure-zone entry this
// engine implements (CR 714.3b's exiled-by-some-other-effect shape is out of
// scope), so the gate is the provenance itself, derived from the log exactly
// like warpRecastAvailable: walking backwards from the log's end,
//
//  1. the most recent MoveZone taking id to exile,
//  2. scanning back before it, the FIRST event of id encountered must be the
//     adventure-flagged CastInfo -- any MoveZone of id in between (the card
//     left exile and returned by some other route: a recast that resolved to
//     the battlefield, a death, a later exile) means the exile did not come
//     from an Adventure resolution, and an unflagged CastInfo means a later
//     cast reset the provenance.
//
// Everything is a fixed-order walk of the log, so a replayed game derives the
// same answer.
func (e *Engine) adventureZoneAvailable(id state.ObjID) bool {
	log := e.L.Events
	exileIdx := -1
	for i := len(log) - 1; i >= 0; i-- {
		if ev := log[i]; ev.Kind == events.MoveZone && ev.Obj == id && ev.To == state.ZExile {
			exileIdx = i
			break
		}
	}
	if exileIdx < 0 {
		return false
	}
	for i := exileIdx - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind == events.MoveZone && ev.Obj == id {
			return false
		}
		if ev.Kind == events.CastInfo && ev.Obj == id {
			return events.FlagsFrom(ev.Counter)&state.FlagAdventure != 0
		}
	}
	return false
}
