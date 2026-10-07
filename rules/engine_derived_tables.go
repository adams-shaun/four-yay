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
	// name filters read through SpecContext.Layers.EffectiveNames. It is refreshed
	// after each emitted event, under active()'s own (epoch, version) key,
	// and only when setNameInPool says this match has a SetName$ carrier at
	// all. It is a FIELD rather than a lazily-called derivation because
	// specCtxSVars must stay inlinable: a call there makes its Resolve
	// closure escape and allocates on every hot-path context construction.
	// Clone copies the table (the clone's board is identical at the clone
	// boundary) and the two key fields with it.
	renames        []effects.ObjectName `clone:"deep"`
	renameEpoch    int                  `clone:"deep"`
	renameVersion  int                  `clone:"deep,rekey"`
	renameObjs     int                  `clone:"deep"`
	renameDSeq     uint64               `clone:"reset"` // derivedSeq at the build; never cloned (0 = none)
	renameBFSeq    uint64               `clone:"reset"` // derivedBFSeq at the build; never cloned
	renameBuilding bool                 `clone:"reset"`
	// renameBranchCounts is the E2 measurement diagnostic (emit-action.md
	// §3 step 1): which exit refreshRenames took, indexed
	// reentry/epoch/inert/gate/derived/rebuild. Reset on clone; never read
	// by rules.
	renameBranchCounts [6]int `clone:"reset"`
	// derivedTypes is the layer-4 derived type table (layer4types.go) the
	// effects tier's ordinary type filters read through SpecContext.
	// DerivedTypes. Exactly the shape (and rationale) of renames above: a
	// field refreshed after each emitted event under active()'s key, gated on
	// layer4InPool, and bound by a plain field read so specCtxSVars stays
	// inlinable. Clone copies the table and its key fields.
	layer4Types   []effects.ObjectTypes `clone:"deep"`
	typesEpoch    int                   `clone:"deep"`
	typesVersion  int                   `clone:"deep,rekey"`
	typesObjs     int                   `clone:"deep"`
	typesBuilding bool                  `clone:"reset"`
	// The incremental layer-4 state (layer4types.go). typesIncrReady is set
	// once a whole-board build has repopulated it; every refresh then first
	// tries refreshDerivedTypesIncremental, which folds the events logged
	// since the last build into typesMayDiffer (the candidate slice) and
	// re-derives only the self-only source list -- never a whole-board scan
	// (the staticsMayChangeTypes precheck has its own probe cache below).
	// Every fallback is toward the whole-board rebuild, never away from it.
	// Clone copies these with the table (clone.go) when the table was built
	// under the current registry: the clone's board is identical at the
	// boundary, so the candidate slice and source stamp describe it exactly
	// and its next refresh can go incremental instead of rebuilding the
	// whole board once per clone.
	typesIncrReady bool `clone:"deep,if=typesIncrReady"`
	typesSelfOnly  bool `clone:"deep,if=typesIncrReady"`
	// typesDSeq is derivedSeq right after the last bounded build (active()
	// current there), typesDSeqOK marks it set; the derived-quiet reuse
	// (typesQuietReuse) compares it. Never cloned: a clone's derivedSeq is
	// its own.
	typesDSeq      uint64        `clone:"reset"`
	typesDSeqOK    bool          `clone:"reset"`
	typesSrcs      []state.ObjID `clone:"deep,if=typesIncrReady"`
	typesMayDiffer []state.ObjID `clone:"deep,if=typesIncrReady"`
	typesTouch     []state.ObjID `clone:"reset"`
	// typesAct is the reusable live-LType-effect buffer the incremental build
	// passes to typeCharacteristicsActive; typesVisited counts the objects the
	// last build examined (the whole board on a full rebuild, the candidate
	// set on an incremental one). The scaling pin reads typesVisited; the
	// engine never does.
	typesAct     []ContinuousEffect `clone:"reset"`
	typesVisited int                `clone:"reset"`
	// The staticsMayChangeTypes probe cache: per-object probe answers with
	// the count of true ones, maintained by the same event-referent catch-up
	// (see layer4types.go). typesProbe is dense by ObjID (index id-1):
	// probeUnset for an object never probed, else probeNo/probeYes;
	// typesProbeReady marks a populated cache.
	typesProbe        []uint8 `clone:"deep,if=typesProbeReady,pool=probe"`
	typesProbeReady   bool    `clone:"deep,if=typesProbeReady"`
	typesProbeTrue    int     `clone:"deep,if=typesProbeReady"`
	typesProbeEpoch   int     `clone:"deep,if=typesProbeReady"`
	typesProbeVersion int     `clone:"deep,if=typesProbeReady,rekey=now"`
	typesProbeObjs    int     `clone:"deep,if=typesProbeReady"`
	// typesIncrBuilds counts successful incremental rebuilds; the scaling
	// tests read it to prove the incremental path (not the whole-board
	// fallback) served a refresh. Never read by the engine itself.
	typesIncrBuilds int `clone:"reset"`
}
