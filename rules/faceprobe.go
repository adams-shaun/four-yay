package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// faceprobe.go: pricing an alternate-face cast offer AS the face it casts.
//
// Every alternate-face cast route (adventure_alt, adventure_recast, room_alt,
// split_alt, aftermath) is consumed by beginCast, which emits a FlipFace to
// the chosen face BEFORE the ordinary cast transaction. From that point on
// every cost reader -- the CR 601.2f cost-modifier gates (a RaiseCost's
// ValidCard$ Card.nonCreature, a ValidSpell$ type/cmc/colour read), the
// commander tax, the card's own self statics -- sees the FLIPPED face,
// because o.Face() is Faces[FaceIdx].
//
// The offer walk used to price those routes with the flipped face's printed
// cost but the object still showing its CURRENT face, so every modifier that
// reads the card's characteristics matched the wrong face. Measured: an
// Adventure creature in hand (Order of Midnight // Alter Fate) under Thalia,
// Guardian of Thraben's "noncreature spells cost {1} more" was offered its
// Alter Fate cast at {1}{B} (the Creature front is not taxed), and the cast
// flow -- reading the flipped Sorcery face -- charged {2}{B}, found it
// unpayable and reversed it (CR 733.1). Nothing changed, so the same offer
// came back and the bot picked it forever (a botbench livelock).
//
// offerAsFace answers the offer question the way the cast will: it points
// the object at the face the cast flips to for the duration of fn and puts
// it back before returning. This is a scoped READ, not a state change: the
// offer walk is a pure read (it emits no event), nothing can observe the
// probed face after the call, and the face the object shows afterwards is
// byte-identical to before -- so the log, the hash chain and replay are
// untouched. The walk's Derived memo (derivedmemo.go) is keyed on the board
// and would otherwise serve (or record) this object's characteristics under
// the wrong face, so it is bypassed for the probe; if anything inside fn
// opened a fresh memo generation, the walk's generation is bumped again on
// the way out so no probed-face entry is ever served after the restore.
func (e *Engine) offerAsFace(id state.ObjID, face *cards.Face, fn func() bool) bool {
	o := e.G.Obj(id)
	if o == nil || o.Card == nil || face == nil {
		return false
	}
	idx := -1
	for i, f := range o.Card.Faces {
		if f == face {
			idx = i
			break
		}
	}
	if idx < 0 {
		// Not one of the object's own faces: nothing to point it at, so price
		// against the live face exactly as before.
		return fn()
	}
	if int(o.FaceIdx) == idx {
		return fn()
	}
	// Bring the log-head-keyed layer caches (active()'s list and the
	// staticEffects memo under it, the layer-4 type table; layercache.go) up
	// to date BEFORE the flip: the probe emits nothing, so they are then hits
	// throughout it, and a first build can never happen inside it and outlive
	// it carrying the probed face.
	_ = e.active()
	e.refreshDerivedTypes()
	prevFace, prevDepth, prevGen := o.FaceIdx, e.derivedMemoDepth, e.derivedMemoGen
	o.FaceIdx = uint8(idx)
	e.derivedMemoDepth = 0
	// The object classes (walk_objclass.go) cache nothing read under the
	// flip, whose fingerprint would outlive it.
	e.offerProbeDepth++
	// The flip is a no-event write the cross-walk memo cannot see (it keys on
	// activeBuildSeq, which a face flip does not move): retire every entry at
	// both edges so a live-face entry is never served inside the probe and a
	// probed-face entry never outlives it.
	e.retireCrossWalkMemo()
	defer func() {
		e.offerProbeDepth--
		o.FaceIdx = prevFace
		e.derivedMemoDepth = prevDepth
		e.retireCrossWalkMemo()
		if e.derivedMemoGen != prevGen {
			e.derivedMemoGen++
		}
	}()
	return fn()
}

// faceHasCostStatics reports whether f carries a cost-modifier static
// (RaiseCost/ReduceCost/SetCost/OptionalCost) -- the only case in which the
// walk's cached cost-static collection, built with the object at its live
// face, differs from one built with the object at f.
func faceHasCostStatics(f *cards.Face) bool {
	if f == nil {
		return false
	}
	for _, st := range f.Statics {
		switch st.Mode {
		case "RaiseCost", "ReduceCost", "SetCost", "OptionalCost":
			return true
		}
	}
	return false
}
