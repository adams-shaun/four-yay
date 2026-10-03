package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// chaosEnsuesMatches implements Mode$ ChaosEnsues (CR 901.9): the current
// plane's chaos ability fires on the events.ChaosEnsues marker the roll
// dispatch and the DB$ ChaosEnsues verb emit. The marker already names the
// plane it erupts on (ev.Obj) and the seat whose planar deck owns it
// (ev.Player), and checkChaosEnsuesTriggers has already narrowed the walk to
// that plane, so the matcher only has to confirm the marker is about THIS
// source. A bare marker with no plane (a Describe-coverage fuzz event)
// matches nothing.
func chaosEnsuesMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.ChaosEnsues || ev.Obj == 0 {
		return false
	}
	return ev.Obj == source
}

// planeswalkedToMatches implements Mode$ PlaneswalkedTo (CR 901.8): "When you
// planeswalk to CARDNAME", fired by the synthetic plane scan below on the
// plane the walk arrived at (ev.IDs[0], or the new current plane for an
// ordinary rotation). The arriving plane is the trigger's source.
func planeswalkedToMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.PlanarWalk {
		return false
	}
	return e.ArrivingPlane(ev) == source
}

// planeswalkedFromMatches implements Mode$ PlaneswalkedFrom (CR 901.8): "When
// you planeswalk away from CARDNAME", fired by the synthetic plane scan below
// on the plane the walk left (ev.Obj). A walk whose DontPlaneswalkAway$ flag is
// set does not fire it (Norn's Seedcore's "don't planeswalk away from any
// plane"): the scanner never queues the ability at all in that case, and this
// matcher rejects it too as a second boundary for a hand-built event.
func planeswalkedFromMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	if ev.Kind != events.PlanarWalk || ev.Obj == 0 || ev.Obj != source {
		return false
	}
	return ev.Amount != events.PlanarWalkDontPlaneswalkAway
}
