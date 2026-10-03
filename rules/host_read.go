package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// host_read.go is the engine's implementation of effects.HostRead, the read
// role of effects.Host (rules-engine refactor spec W1d): the live game and
// the characteristics queries an effect reads it through.

// boardLayers is the always-published pair of the board's derived-
// characteristic tables: the layer-3 rename table (setname.go) and the
// layer-4 derived type table (layer4types.go), both refreshed after every
// emitted event. The other LayerTables (static goads, layer-5 colours,
// layer-6 keywords) are built on demand; LayerTables adds them.
func (e *Engine) boardLayers() effects.LayerTables {
	return effects.LayerTables{EffectiveNames: e.renames, DerivedTypes: e.layer4Types}
}

// LayerTables is effects' optional layerTablesHost: the board's derived-
// characteristic tables a resolving Ctx binds (effects.Resolve reads it at
// walk entry and at every body boundary). The rename and type tables always;
// the static-goad set, the layer-5 colour and layer-6 keyword tables only when
// want asks, because rules builds those on demand.
func (e *Engine) LayerTables(want effects.LayerTableSet) effects.LayerTables {
	t := e.boardLayers()
	if want&effects.LayerGoads != 0 {
		// The static-goad set (staticgoad1): a resolving IsGoaded read agrees
		// with the combat requirement's combat.staticGoaders derivation instead of
		// seeing the event-backed goad list alone.
		t.StaticGoads = e.staticallyGoaded()
	}
	if want&effects.LayerColors != 0 {
		// The layer-5 colour table (layer5colors.go), for a body that names
		// a colour word.
		t.DerivedColors = e.derivedColorTable()
	}
	if want&effects.LayerKeywords != 0 {
		t.DerivedKeywords = e.EffectiveKeywords()
	}
	return t
}

// Chars is effects.HostRead's characteristics query: the object's current,
// layer-derived characteristics (effects.Chars, which Derived aliases). It is
// Derived(id) answered by pointer: inside a Derived memo scope (a legal-actions
// walk, a BeginDerivedReads board build) the pointer is the memo entry
// itself, otherwise it is the engine's one charsScratch record.
//
// The pointer -- and its Keywords/Types -- is valid until the next Chars or
// Derived call or the next emit, whichever comes first; a caller that holds
// characteristics across either copies the record (and the slices it keeps),
// and never makes two Chars reads in one expression: Go does not order the
// first read's field load before the second call, so both can see the second
// record (the api:ExchangeTextBox capture hit exactly this).
// derivedMemoVerify (the rules test binary) recomputes every memo hit, the
// empirical check that a served entry is the current answer.
func (e *Engine) Chars(id state.ObjID) *effects.Chars {
	if e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		if d := e.derivedMemoRef(id, 0); d != nil {
			return d
		}
	}
	e.charsScratch = e.derivedCompute(id, 0)
	return &e.charsScratch
}

func (e *Engine) Game() *state.Game { return e.G }

func (e *Engine) ObjectColors(o *state.Object) string { return e.objColors(o) }

// CastProhibited is effects' optional castProhibitedHost read (task
// play-prohibited-election): the same CantBeCast gate beginPlay enforces
// (rules/statics.go castRestricted), exposed so effPlay's Play election never
// OFFERS a cast that CR 601.3 would refuse. It is a pure read -- no event,
// no state change -- and the beginPlay recheck stays the enforcement site.
func (e *Engine) CastProhibited(p state.PlayerID, id state.ObjID) bool {
	return e.castRestricted(p, id)
}
