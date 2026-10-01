package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// engineDerivedTables groups the Engine's layer-3 rename and layer-4
// derived-type tables and their keys. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineDerivedTables struct {
	// name filters read through SpecContext.EffectiveNames. It is refreshed
	// after each emitted event, under active()'s own (epoch, version) key,
	// and only when setNameInPool says this match has a SetName$ carrier at
	// all. It is a FIELD rather than a lazily-called derivation because
	// specCtxSVars must stay inlinable: a call there makes its Resolve
	// closure escape and allocates on every hot-path context construction.
	// Clone copies the table (the clone's board is identical at the clone
	// boundary) and the two key fields with it.
	renames        []effects.ObjectName
	renameEpoch    int
	renameVersion  int
	renameObjs     int
	renameBuilding bool
	// derivedTypes is the layer-4 derived type table (layer4types.go) the
	// effects tier's ordinary type filters read through SpecContext.
	// DerivedTypes. Exactly the shape (and rationale) of renames above: a
	// field refreshed after each emitted event under active()'s key, gated on
	// layer4InPool, and bound by a plain field read so specCtxSVars stays
	// inlinable. Clone copies the table and its key fields.
	layer4Types   []effects.ObjectTypes
	typesEpoch    int
	typesVersion  int
	typesObjs     int
	typesBuilding bool
	// The incremental layer-4 state (layer4types.go). typesIncrReady is set
	// once a whole-board build has repopulated it; every refresh then first
	// tries refreshDerivedTypesIncremental, which folds the events logged
	// since the last build into typesMayDiffer (the candidate slice) and
	// re-derives only the self-only source list -- never a whole-board scan
	// (the staticsMayChangeTypes precheck has its own probe cache below).
	// Every fallback is toward the whole-board rebuild, never away from it.
	// Clone deliberately copies none of these: the clone's board is
	// identical at the boundary, so the carried table + key above stays
	// valid for key hits, and the clone's first real rebuild repopulates
	// the incremental state from scratch (the activeEpoch precedent).
	typesIncrReady bool
	typesSelfOnly  bool
	typesSrcs      []state.ObjID
	typesMayDiffer []state.ObjID
	typesTouch     []state.ObjID
	// typesAct is the reusable live-LType-effect buffer the incremental build
	// passes to typeCharacteristicsActive; typesVisited counts the objects the
	// last build examined (the whole board on a full rebuild, the candidate
	// set on an incremental one). The scaling pin reads typesVisited; the
	// engine never does.
	typesAct     []ContinuousEffect
	typesVisited int
	// The staticsMayChangeTypes probe cache: per-object probe answers with
	// the count of true ones, maintained by the same event-referent catch-up
	// (see layer4types.go).
	typesProbe        map[state.ObjID]bool
	typesProbeTrue    int
	typesProbeEpoch   int
	typesProbeVersion int
	typesProbeObjs    int
	// typesIncrBuilds counts successful incremental rebuilds; the scaling
	// tests read it to prove the incremental path (not the whole-board
	// fallback) served a refresh. Never read by the engine itself.
	typesIncrBuilds int
}
