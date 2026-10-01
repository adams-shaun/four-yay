package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// objectContinuousHot is the static memo's own hotness test (layercache.go's
// staticMoveCold, staticLibraryOrderCold and the appended-object check): it
// reports whether staticEffectsWalk could emit anything from, or evaluate a
// gate of, o while o sits in zone z. It is finer than the static zone
// skip's shared objectStaticHot, which serves every static collector and so
// treats a face with ANY static (on the battlefield) or any EffectZone$ /
// type-changing static (off it) as hot; the walk itself only ever acts on a
// Mode$ Continuous static. Every face o.Face() can resolve to is checked --
// each entry of o.Card.Faces (a flip, a transform, an unlocked Room's other
// half) and o.CopyFace -- and a merged pile is always hot, exactly as in
// objectStaticHotOn:
//
//   - on the battlefield the walk skips a static whose Mode is not
//     Continuous before reading anything else of it, so a face carrying no
//     Continuous static is inert there;
//   - off the battlefield the walk skips an object whose face answers false
//     to cards.Face.ContinuousStaticsMayFunctionOffBattlefield (a
//     conservative probe: unbound or stale answers true) before anything
//     else, the stack included.
func objectContinuousHot(o *state.Object, z state.Zone) bool {
	if o == nil {
		return false
	}
	if len(o.MergedCards) > 0 {
		return true
	}
	if faceContinuousHot(o.CopyFace, z) {
		return true
	}
	if o.Card != nil {
		for _, f := range o.Card.Faces {
			if faceContinuousHot(f, z) {
				return true
			}
		}
	}
	return false
}

func faceContinuousHot(f *cards.Face, z state.Zone) bool {
	if f == nil {
		return false
	}
	if z != state.ZBattlefield {
		return f.ContinuousStaticsMayFunctionOffBattlefield()
	}
	for i := range f.Statics {
		if f.Statics[i].Mode == "Continuous" {
			return true
		}
	}
	return false
}

// Object-local kinds. staticSafeSince admits Imprint and Choose for a quiet
// (or gate-rechecked) build through staticMoveCold: their Apply writes only
// fields of the event's own object (Imprint: its Imprinted /
// ExiledCards / ImprintTokens / SeekFound / EncodedCards / ExileReturn lists;
// Choose: its Chosen* answers, Remembered, mode picks and noted mana). The
// scan reads an object's Chosen* and Imprinted only while emitting that
// object's own statics, so when the object cannot contribute where it sits
// (and is no memo source) the scan is unchanged; a later move that makes it
// hot is itself refused, and the rescan then reads the new values. A gate
// that reads them (a Remembered spec) is a staticGateDepAny gate and is
// re-evaluated on every admitted run.

// staticGatesScratchProof reports whether the memo's build is gated only by
// gates whose read set is exactly known (no staticGateDepAny record) and made
// no other state read: the exclusion scratches invalidateScratchLayerLists
// guards (costCompositionEvent, stackGrantCast, an expired casualty grant)
// reach none of the active player, a life total or a runtime SVar, so the
// memo -- and the active list built from it -- is the same under them.
func (e *Engine) staticGatesScratchProof() bool {
	if !e.staticGatesKnown || e.staticMemoStateRead {
		return false
	}
	for i := range e.staticGates {
		if e.staticGates[i].dep == staticGateDepAny {
			return false
		}
	}
	return true
}
