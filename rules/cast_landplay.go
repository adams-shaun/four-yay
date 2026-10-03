package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// hasRevealChosenDesignation reports whether o still carries the
// secretly-chosen designation a RevealChosen<Spec> part names: a player entry
// in Object.Chosen for RevealChosen<Player> (the Secretly$ True ChoosePlayer
// answer), a non-empty Object.ChosenType for RevealChosen<Type/...> (the
// Secretly$ True ChooseType answer). A RevealChosen part has no alternative
// payment -- no hand card is picked -- so an unset designation makes the whole
// cost unpayable and the ability is not offered at all.
func hasRevealChosenDesignation(o *state.Object, spec string) bool {
	if o == nil {
		return false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return true
			}
		}
		return false
	}
	return o.ChosenType != ""
}

// revealChosenText composes the public reveal line a RevealChosen<Spec> part
// prints as it is paid. The chosen player's identity is the chain-safe
// tossName -- the deck-identity Name, never the display PlayerName (the F3
// invariant every other event text keeps; view/describe.go's player label may
// prefer PlayerName, but that is a view projection, not chain text).
func revealChosenText(g *state.Game, o *state.Object, spec string) (string, bool) {
	if o == nil {
		return "", false
	}
	if strings.EqualFold(spec, "Player") {
		for _, t := range o.Chosen {
			if t.IsPlayer {
				return "revealed the chosen player: " + tossName(g, t.Player), true
			}
		}
		return "", false
	}
	if o.ChosenType == "" {
		return "", false
	}
	return "revealed the chosen creature type: " + o.ChosenType, true
}

// finishLandPlay logs a land play only after its identified object actually
// reaches the battlefield. Updated replacement effects fold their MoveZone
// directly through events.Emit, so both the ordinary emit path and those
// replacement continuations call this one finalizer.
func (e *Engine) finishLandPlay(id state.ObjID) {
	if !e.etbLandPlay || id != e.etbLandObj {
		return
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		return
	}
	e.settleLandPlay(id)
}

// settleLandPlay closes the land play's accounting for id and disarms the
// continuation, WITHOUT requiring the land to be on the battlefield. Playing a
// land is a special action that is taken -- and so uses that turn's land play
// (CR 305.1, CR 505.5b) -- the moment it is announced; a replacement effect
// that fully replaces the land's battlefield entry (ReplacementResult$
// Replaced) changes where the land ends up, not whether it was played. Leaving
// the continuation armed instead would both hand the player a second land play
// that turn and let a LATER, unrelated entry of the same object consume the
// stale continuation and log a LandPlayed that belongs to nothing.
func (e *Engine) settleLandPlay(id state.ObjID) {
	if !e.etbLandPlay || id != e.etbLandObj {
		return
	}
	p := e.etbLandPlayer
	e.etbLandPlay = false
	e.etbLandObj = 0
	e.emit(events.Event{Kind: events.LandPlayed, Player: p})
}

// landEntryParked reports whether some engine machinery still holds id's
// battlefield entry and will re-emit it once an outstanding answer arrives: an
// as-enters choice parked by applyETBChoiceReplacement (or the riot/unleash/
// siege fallbacks that park the same way), or a replacement body suspended on a
// mid-resolution ask. While the entry is parked the land play is not settled --
// the entry is still in flight and its own completion reaches finishLandPlay.
func (e *Engine) landEntryParked(id state.ObjID) bool {
	for _, parked := range [...]*events.Event{e.etbMove, e.riotMove, e.unleashMove, e.siegeMove} {
		if parked != nil && parked.Obj == id {
			return true
		}
	}
	return false
}

// settleLandPlayIfDone is the terminal-path settle: called wherever a land
// play's battlefield entry has finished being processed, it logs the land play
// unless the entry is still parked on an outstanding answer. An entry that
// completed normally already settled through finishLandPlay, so this is a
// no-op there; it fires exactly for the fully-replaced entry, which never
// reaches the battlefield at all.
func (e *Engine) settleLandPlayIfDone(id state.ObjID) {
	if e.landEntryParked(id) {
		return
	}
	e.settleLandPlay(id)
}
