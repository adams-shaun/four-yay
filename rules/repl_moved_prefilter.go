package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Moved-line prefilter.
//
// The replacement dispatch visits every replacement-hot object with a Moved
// line on every MoveZone, and most of those lines are a permanent's own
// entry replacement (an ETB-tapped land, an etbCounter creature) sitting in
// a library or hand: `ValidCard$ Card.Self ... Destination$ Battlefield`.
// For any other object's move such a line can never match, but reaching
// that verdict through replacementMatches pays the ActiveZones$ gate, the
// cast-provenance split, a spec context and a filter match first.
//
// movedLineRejects decides two of the Moved arm's own predicates up front:
// the Destination$ zone (a static parameter against ev.To) and a self-only
// ValidCard$ (every alternative of the spec carries the Self property, whose
// predicate is o.ID == source) against ev.Obj. The Moved arm is a pure
// conjunction of predicates, so a line either test rejects is one the full
// matcher rejects too. replZoneSkipVerify runs the full matcher on every
// rejected line and panics if it would have matched.
func movedLineRejects(r *cards.Repl, source state.ObjID, ev events.Event) bool {
	if r.Event != "Moved" {
		return false
	}
	if d, ok := r.ParamCode(cards.PKDestination); ok && !effects.Destination(d).IsAny() && effects.Destination(d).Zone() != ev.To {
		return true
	}
	if ev.Obj != source {
		if v, ok := r.Param(cards.PKValidCard); ok && specSelfOnly(v) {
			return true
		}
	}
	return false
}

// specSelfOnly reports whether a filter spec can admit only its own source:
// it has no alternatives (no comma) and its property chain (the '+'-joined
// list after the first '.') carries the bare Self property.
func specSelfOnly(spec string) bool {
	if strings.IndexByte(spec, ',') >= 0 {
		return false
	}
	dot := strings.IndexByte(spec, '.')
	if dot < 0 {
		return false
	}
	props := spec[dot+1:]
	for props != "" {
		tok := props
		if i := strings.IndexByte(props, '+'); i >= 0 {
			tok, props = props[:i], props[i+1:]
		} else {
			props = ""
		}
		if tok == "Self" {
			return true
		}
	}
	return false
}
