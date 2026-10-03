package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// planeswalk.go is planeswalking (CR 901.8): shuffling the planar deck and
// moving a player to a new plane.

// shufflePlanarDeck uses the match RNG; the emitted event, rather than this
// temporary order, is the replayable state change.
func (e *Engine) shufflePlanarDeck(player state.PlayerID, order []state.ObjID) []state.ObjID {
	out := append([]state.ObjID(nil), order...)
	e.rng.Shuffle(out)
	return out
}

// Planeswalk rotates this seat's current plane to the bottom and reveals the
// next plane (CR 901.8). With fewer than two cards there is no next plane to
// reveal. The event names the plane being walked away from (Obj) so the away
// trigger has a source; the walk itself is the PlanarWalk fold.
func (e *Engine) Planeswalk(player state.PlayerID) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away})
}

// PlaneswalkTo walks this seat to the named destination plane(s) instead of
// rotating the deck (CR 901.8's Defined$ planeswalk; Norn's Seedcore,
// Spatial Merging). dontPlaneswalkAway suppresses the PlaneswalkedFrom
// ability of the plane being left. A game with no planar deck, or with no
// destination still in the zone, records nothing: there is no plane to walk
// to. The event's Obj names the departing plane and IDs the destinations, so
// the fold in events/apply.go moves exactly those planes to the current
// position.
func (e *Engine) PlaneswalkTo(player state.PlayerID, dests []state.ObjID, dontPlaneswalkAway bool) {
	if int(player) >= len(e.G.Players) || len(e.G.Zone(state.ZPlanarDeck, player)) == 0 {
		return
	}
	zone := e.G.Zone(state.ZPlanarDeck, player)
	kept := make([]state.ObjID, 0, len(dests))
	for _, d := range dests {
		for _, id := range zone {
			if id == d {
				kept = append(kept, d)
				break
			}
		}
	}
	if len(kept) == 0 {
		return
	}
	var away state.ObjID
	if p := e.currentPlane(player); p != nil {
		away = p.ID
	}
	var amount int32
	if dontPlaneswalkAway {
		amount = events.PlanarWalkDontPlaneswalkAway
	}
	e.emit(events.Event{Kind: events.PlanarWalk, Player: player, Obj: away, Amount: amount, IDs: kept})
}
