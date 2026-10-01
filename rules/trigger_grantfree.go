package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The trigger walk's granted-static list (grantedTriggerStaticsFor over
// active()) is built on every emitted event, and active() rebuilds after
// almost every event, so for the overwhelming majority of matches -- whose
// cards grant no triggered ability at all -- the walk paid a whole active()
// rebuild per event to learn the list is empty.
//
// An active() entry can carry a granted trigger (AddTrigger or
// GainedTriggerFaces) only from:
//
//   - a registered continuous effect (AddContinuous, the one append site of
//     e.continuous): AddContinuous ends the proof (trigGrant.free) on such an
//     entry;
//   - the static scan (layers.go): a Continuous static's AddTrigger$ or
//     GainsTriggerAbsOf$ parameter, on a face of an object or a static
//     granted from an SVar of one (the grant queue parses SVar text).
//
// Every face the static scan reads is a face of some object: its card's
// faces, its CopyFace (built from another object's face, plus statics parsed
// from SVar text of such a face), or a merged card's (another object's card).
// So while no object's faces carry either parameter on a static, or either
// word in an SVar, and no registered effect carries a grant, the list is
// empty. Genesis starts the proof and it is held incrementally: every object
// added since the last check (a created token, a fixture's card) and a
// changed e.continuous header (anything but AddContinuous writing the
// registry) is checked when the walk next asks, and any hit ends the proof
// for the rest of the match. Clone copies it; any other engine starts
// without it. trigZoneSkipVerify builds the list anyway and panics if it is
// not empty.

type trigGrantProof struct {
	free    bool
	objs    int // e.G.Objs[:objs] checked
	contPtr *ContinuousEffect
	contLen int // e.continuous header last checked (-1: never)
	// replSeen: some registered continuous effect carried an effect-created
	// replacement (ReplacementEvent), which only registrations carry -- the
	// static scan never sets one, so active() can hold one only from
	// e.continuous. Sticky; while it is false, applyReplacementsDispatch's
	// effect-created half reads an empty list without building active().
	// Set by AddContinuous and by the registry rescan, which every header
	// change of e.continuous triggers (an empty registry has nothing to see).
	replSeen bool
}

// forClone is the proof a Clone inherits: the same objects (copied in
// order), over its own copy of the registry, which it rechecks once (the
// sticky flags carry over: the clone's registry is the parent's).
func (p trigGrantProof) forClone() trigGrantProof {
	p.contPtr, p.contLen = nil, -1
	return p
}

// trigGrantsPossible reports whether some active() entry may carry a granted
// trigger (false only while the proof holds).
func (e *Engine) trigGrantsPossible() bool {
	pr := &e.trigGrant
	if !pr.free {
		return true
	}
	if !e.registryRescan() {
		return true
	}
	for ; pr.objs < len(e.G.Objs); pr.objs++ {
		o := &e.G.Objs[pr.objs]
		if e.faceGrantsTriggersCached(o.CopyFace) {
			pr.free = false
			return true
		}
		if o.Card != nil {
			for _, f := range o.Card.Faces {
				if e.faceGrantsTriggersCached(f) {
					pr.free = false
					return true
				}
			}
		}
	}
	return false
}

// registryRescan rechecks e.continuous when its header moved since the last
// check (any write AddContinuous did not make: a filter, a fixture's direct
// assignment) and reports whether the trigger-grant proof still holds.
func (e *Engine) registryRescan() bool {
	pr := &e.trigGrant
	if n := len(e.continuous); n != pr.contLen || (n > 0 && &e.continuous[0] != pr.contPtr) {
		for i := range e.continuous {
			ce := &e.continuous[i]
			if ce.AddTrigger != nil || len(ce.GainedTriggerFaces) > 0 {
				pr.free = false
			}
			if ce.ReplacementEvent != "" {
				pr.replSeen = true
			}
		}
		pr.contLen, pr.contPtr = n, nil
		if n > 0 {
			pr.contPtr = &e.continuous[0]
		}
	}
	return pr.free
}

// effectReplacementsPossible reports whether active() may hold an
// effect-created replacement (false while none was ever registered).
func (e *Engine) effectReplacementsPossible() bool {
	if !e.trigGrant.replSeen {
		e.registryRescan()
	}
	return e.trigGrant.replSeen
}

func (e *Engine) faceGrantsTriggersCached(f *cards.Face) bool {
	if f == nil {
		return false
	}
	if ff := e.walkFaceFactsOf(f); ff != nil && ff.triggersCurrent(f) {
		return ff.grantsTrig
	}
	return faceGrantsTriggers(f)
}

func faceGrantsTriggers(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if strings.TrimSpace(st.ParamStr(cards.PKAddTrigger)) != "" || strings.TrimSpace(st.Params["GainsTriggerAbsOf"]) != "" {
			return true
		}
	}
	for _, v := range f.SVars {
		if strings.Contains(v, "AddTrigger") || strings.Contains(v, "GainsTriggerAbsOf") {
			return true
		}
	}
	return false
}
