package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// airbendCastAvailable reports whether the card id -- which the caller has
// already established sits in its owner's exile zone -- may be cast for {2}
// rather than its mana cost under CR 701.65a ("for as long as it remains
// exiled, its owner may cast it by paying {2} rather than its mana cost").
//
// The permission is log-derived, never mutable per-object state: the airbend
// exile is the MoveZone whose Counter carries effects.AirbendExileCounter
// (effects/airbend.go), and the permission lasts exactly as long as that
// marker-carrying move is the card's most recent move into exile. A card
// re-exiled by anything else fails the scan (the marker is on an older move),
// and a card airbent again simply carries the marker on the newer move. The
// same log-scan shape warpRecastAvailable and foretellCastAvailable take, so a
// replayed game derives the identical answer.
func (e *Engine) airbendCastAvailable(id state.ObjID) bool {
	if !e.airbendLogged() {
		return false
	}
	log := e.L.Events
	for i := len(log) - 1; i >= 0; i-- {
		ev := log[i]
		if ev.Kind != events.MoveZone || ev.Obj != id {
			continue
		}
		// An in-exile card's most recent move is by construction the move
		// that brought it here, so the marker on THAT move is the whole of
		// the permission; a card whose latest move was out of exile (it is
		// on the stack or battlefield again) has none.
		return ev.To == state.ZExile && ev.Counter == effects.AirbendExileCounter
	}
	return false
}
