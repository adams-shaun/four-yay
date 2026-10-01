package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// BoardReadKey is the key an external pure reader caches Characteristics
// answers under across reads (botpolicy's incremental board build). It
// brings active() up to date and returns the Derived memo's own cross-walk
// key (derivedmemo.go, derived_transparent.go): the engine's event log as
// its lineage, derivedSeq, continuousVersion and the object-arena length.
//
// The contract is the cross-walk memo's. Between two reads of the SAME
// engine (the same lineage pointer: a Clone has its own log, and derivedSeq
// is never cloned) at which every component is equal, Characteristics(id)
// answers identically for every object that
//
//   - CharacteristicsReusable reports reusable (the memo never reuses a
//     characteristic-defining face's derivation across walks), and
//   - no event logged between the two reads names as its Obj (the memo
//     retires the entries of an object an off-battlefield move touched while
//     keeping derivedSeq; naming every logged Obj is a superset of that).
//
// ok is false when the memo is not usable here (a read inside a derivation),
// the engine has no log (a hand-built test engine) or no build has stamped a
// key yet; the caller then derives everything.
// It emits nothing and changes no game state: active() is the same cache
// refresh the first Characteristics call of a board build performs anyway.
func (e *Engine) BoardReadKey() (lineage *events.Log, seq uint64, version, objs int, ok bool) {
	if e.L == nil || e.G == nil || !e.derivedMemoUsable() {
		return e.L, 0, 0, 0, false
	}
	e.active()
	return e.L, e.derivedSeq, e.continuousVersion, len(e.G.Objs), e.derivedSeq != 0
}

// CharacteristicsReusable reports whether id's derivation may be reused
// under an unchanged BoardReadKey: false for an object whose face carries a
// characteristic-defining static (faceHasCDAStatic, the cross-walk memo's own
// exclusion: its amount reads arbitrary state). It depends on the object's
// current face only.
func (e *Engine) CharacteristicsReusable(id state.ObjID) bool {
	return !faceHasCDAStatic(e.G.Obj(id))
}
