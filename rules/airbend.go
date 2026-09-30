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
	return buildAirbendExileIndex(e.L.Events).available(id)
}

// airbendIndexOff makes exileCastsWalk answer each exiled card with the
// literal backward log scan (airbendScan) instead of the per-walk index,
// for the offer-surface equivalence test's index-off arm. Default false --
// production always indexes. Mirrors mayPlayCandIndexOff
// (rules/mayplay_index.go).
var airbendIndexOff bool

// airbendScan is the original one-off backward log scan: the latest MoveZone
// naming id decides the permission. It is the reference arm airbendIndexOff
// selects and the shape buildAirbendExileIndex folds into a map.
func airbendScan(log []events.Event, id state.ObjID) bool {
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

// airbendExileIndex answers airbendCastAvailable for every card an offer walk
// asks about, from ONE backward pass over the log. The latest MoveZone naming
// id decides: to exile with effects.AirbendExileCounter is the permission, any
// other latest move withholds it. Built fresh per walk from the append-only
// log, so it is replay-exact and cannot drift mid-walk (no Emit runs between
// the exile loop's offer checks).
type airbendExileIndex map[state.ObjID]bool

// buildAirbendExileIndex folds the log's MoveZone events into one permission
// per card id. The log is scanned FORWARD and every MoveZone overwrites the
// id's entry, so the map holds the latest move's verdict for each id --
// exactly what airbendScan returns for that id. Ids never seen keep no entry
// and available reports false, matching the scan's `return false`.
func buildAirbendExileIndex(log []events.Event) airbendExileIndex {
	ix := make(airbendExileIndex)
	for i := range log {
		ev := log[i]
		if ev.Kind != events.MoveZone {
			continue
		}
		ix[ev.Obj] = ev.To == state.ZExile && ev.Counter == effects.AirbendExileCounter
	}
	return ix
}

// available reports whether the card id carries the airbend recast
// permission. An id the index never saw is false, matching the scan.
func (ix airbendExileIndex) available(id state.ObjID) bool {
	return ix[id]
}
