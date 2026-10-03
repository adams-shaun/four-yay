package trigmatch

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// unattachedMatches implements Mode$ Unattached (CR 701.3b), the
// "whenever CARDNAME becomes unattached from a permanent" trigger the four
// corpus carriers print (Captain's Hook, Grafted Exoskeleton, Grafted
// Wargear, Stitcher's Graft -- all Equipment). The four lines share one
// shape: `ValidAttachment$ Card.Self | ValidObject$ Permanent`.
//
// The event is events.Unattached (NOT an empty-IDs events.Attach): a distinct
// Kind is what lets this mode fire while Mode$ Attached keeps ignoring a
// detach, and the event carries the attachment in Obj and the FORMER BEARER
// in IDs[0]. rules/attach.go's attachmentSBAs is the one emit site for the
// arms where the attachment stays on the battlefield (the bearer left, the
// bearer stopped being a valid bearer, protection, the CR 702.114b bestowed
// type switch) -- the same detach sites its own comment enumerates.
//
// ValidAttachment$ names the ATTACHING object -- Forge's ValidSource$ for
// the detach half -- which is the object that actually became unattached:
// ev.Obj. It is evaluated against ev.Obj, NOT the trigger's source. Every
// corpus carrier spells it `Card.Self`, and Card.Self binds to the trigger's
// own source, so the two coincide exactly for those lines; but a face that
// pends several detaches on the battlefield must gate on the ONE attachment
// the event names, or every Grafted Exoskeleton present would fire when an
// unrelated Equipment detaches (the trigger would sacrifice that unrelated
// event's former bearer). ValidObject$ names the bearer the attachment
// became unattached from: ev.IDs[0]. On the bearer-left path that object has
// already left the battlefield, so matchesUnattachedBearer reads a bare
// `Permanent` base the way the rest of the engine reads a permanent CARD
// away from the battlefield (the leading-token `PermanentCard` rewrite),
// exactly as the Dig windows and the target census already do. A trigger
// naming neither parameter is treated as self-scoped: an attachment-only
// line fires for its own source rather than silently never firing.
func unattachedMatches(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
	if ev.Kind != events.Unattached || ev.Obj == 0 || len(ev.IDs) == 0 {
		return false
	}
	if t.Params["Static"] == "True" {
		// Forge's "static effect expressed as a trigger" guard, the same one
		// attachedMatches carries: a clone/continuous ETB shape must not fire
		// on every detach.
		return false
	}
	ctrl := e.ControllerOf(source)
	if v, ok := t.Params["ValidAttachment"]; ok {
		// ev.Obj is the attachment the event names, exactly as
		// attachedMatches matches its ValidSource$ against ev.Obj. source is
		// what Card.Self binds to in specCtx, so the corpus's Card.Self lines
		// keep their meaning while an unrelated detach can no longer fire
		// this face.
		if !e.MatchesSpec(v, ev.Obj, source, ctrl, SpecOpts{}) {
			return false
		}
	}
	if v, ok := t.Params["ValidObject"]; ok {
		return matchesUnattachedBearer(e, v, ev.IDs[0], source, ctrl)
	}
	return true
}

// matchesUnattachedBearer evaluates a Mode$ Unattached ValidObject$ spec
// against the former bearer. It first tries the ordinary live reading (the
// bearer is still a battlefield permanent on the protection / no-longer-a-
// creature / bestowed detach paths). When that fails and the bearer is no
// longer on the battlefield -- the bearer-left path, where the trigger must
// still fire for the permanent it WAS -- it retries with the leading
// `Permanent` base rewritten to `PermanentCard`, so a creature/battle/
// planeswalker card off the battlefield still matches Forge's "a permanent".
// The rewrite is inert for every non-Permanent base and for a live
// permanent (its face is a permanent type too), so the retry cannot widen a
// spec the live reading already decided.
func matchesUnattachedBearer(e Board, spec string, bearer, source state.ObjID, ctrl state.PlayerID) bool {
	if e.MatchesSpec(spec, bearer, source, ctrl, SpecOpts{}) {
		return true
	}
	o := e.Game().Obj(bearer)
	if o == nil || o.Zone == state.ZBattlefield {
		return false
	}
	return e.MatchesSpec(SpellCastPermanentSpec(spec), bearer, source, ctrl, SpecOpts{})
}

func init() {
	registerTrigMatcher(func(e Board, t cards.Trigger, source state.ObjID, ev events.Event, lki *state.Object) bool {
		return unattachedMatches(e, t, source, ev, lki)
	}, "Unattached")
}
