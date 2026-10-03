// Foretell trigger mode.
//
// Mode$ Foretell: "Whenever a player foretells a card, ..." (CR 702.126b;
// Dream Devourer, the corpus's sole carrier at the pin -- measured
// `/usr/bin/grep -rlE 'T:Mode\$ Foretell' .cards/cardsfolder | wc -l` => 1).
//
// The foretell action's replayable provenance is a two-event sequence in
// payCast's foretell branch (rules/cast.go): a CastInfo stamped
// FlagForetold while the card is still IN ITS HAND, followed by the
// face-down hand->exile MoveZone. The matcher binds the trigger to the
// CastInfo -- the flag alone cannot bind it, because the LATER
// foretell-cost cast from exile (rules/cast.go's modeFlags "foretell_cast")
// stamps the same flag on ITS pay-time CastInfo, and casting a foretold
// card is not foretelling (CR 702.126a: the special action is the exile).
// What distinguishes the two is the card's zone at emit time: the action's
// CastInfo is emitted BEFORE any move, so the card is still in ZHand; the
// later cast's CastInfo is emitted after the up-front PutOnStack, so the
// card is on the stack. A second provenance shape exists -- an EFFECT's
// Foretold$ True designation (effects/zone.go's applyFaceDownMarker), which
// emits no CastInfo at all, only the exile MoveZone whose counter carries
// the designation -- and it matches the same mode. The action's own
// MoveZone (plain "exiled_with_face_down") must NOT match here: its event
// already fired the CastInfo arm, and matching it again would double-fire
// the trigger on every foretell.
//
// Split into its own trigmatch file so tickets touching different modes
// stop colliding on one file. Registration is at the bottom; a duplicate
// mode panics (registerTrigMatcher).

package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// foretellMatches implements Mode$ Foretell. The foretelling player is the
// foretold card's controller -- in a hand a card is controlled by its owner,
// and CR 702.126a's action is the owner's own, so the read is the same seat
// the action's {2} came from (the action's CastInfo carries no Player field
// by its pinned wire encoding, rules/foretell_adjacent_test.go's
// TestForetellActionEventEncodingUnchanged, so the controller is the
// replay-stable way to name the foreteller). An effect that designates an
// exiled card foretold from someone else's hand therefore names THAT hand's
// owner, never the exiling effect's controller.
func foretellMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, _ *state.Object) bool {
	var o *state.Object
	switch ev.Kind {
	case events.CastInfo:
		// The {2} action's pay-time marker: FlagForetold while the card is
		// still in its hand. The later foretell-cost cast's CastInfo carries
		// the same flag after the card has moved exile->stack, so the zone
		// read is the whole of the action/cast distinction.
		if events.FlagsFrom(ev.Counter)&state.FlagForetold == 0 {
			return false
		}
		o = e.Game().Obj(ev.Obj)
		if o == nil || o.Zone != state.ZHand {
			return false
		}
	case events.MoveZone:
		// The effect-designation markers (applyFaceDownMarker's
		// Foretold$ True composition, with and without WithMayLook$). The
		// {2} action's plain "exiled_with_face_down" counter is excluded by
		// this exactness, not by accident.
		if ev.To != state.ZExile ||
			(ev.Counter != "exiled_with_face_down_foretold" &&
				ev.Counter != "exiled_with_face_down_maylook_foretold") {
			return false
		}
		o = e.Game().Obj(ev.Obj)
		if o == nil {
			return false
		}
	default:
		return false
	}
	if int(o.Controller) >= len(e.Game().Players) {
		return false
	}
	ctrl := e.ControllerOf(source)
	if v := t.ParamStr(cards.PKValidPlayer); v != "" &&
		!effects.MatchesPlayerSpec(e.Game(), v, o.Controller, ctrl) {
		return false
	}
	if v := t.ParamStr(cards.PKValidCard); v != "" &&
		!e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
		return false
	}
	return true
}

func init() {
	registerTrigMatcher(foretellMatches, "Foretell")
	effects.RegisterNonAPI("trig:Foretell")
}
