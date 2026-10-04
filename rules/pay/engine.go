package pay

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// Engine is everything the payment layer reads from, or asks of, the rules
// engine (lasagna spec §9, E7). Package rules implements it on a pointer
// conversion of its *Engine (rules' asPayer), so passing it allocates nothing.
// Every state mutation still goes through Emit, which is rules' emit: the
// payment layer never writes a state.Game field.
type Engine interface {
	// Game is the live match state, for reading.
	Game() *state.Game
	// Emit applies ev through events.Apply and records it.
	Emit(ev events.Event) events.Event
	// Log is the match's event log, for reading (what was produced, drawn
	// or cast this turn); every append goes through Emit.
	Log() *events.Log
	// Verify reports whether the engine's cache/fast-path verify mode is on
	// (rules' walkCacheVerify): a fast path then recomputes the slow answer
	// and panics on disagreement.
	Verify() bool

	// PayLifeInsteadOfB reports whether p pays under a PayLifeInsteadOf:B
	// static (K'rrik): every plain {B} pip also accepts 2 life.
	PayLifeInsteadOfB(p state.PlayerID) bool
	// MayPlayRider is the may-play grant riders (MayPlayIgnoreColor$,
	// MayPlayIgnoreType$) p's active grants extend to card id in its current
	// zone.
	MayPlayRider(p state.PlayerID, id state.ObjID) PipRider
	// Conv is the stat:ManaConvert conversion set for p paying for id
	// (ability selects ValidSA$ scoping), or nil when nothing converts.
	Conv(p state.PlayerID, id state.ObjID, ability bool) *Conv

	// MatchesSpecFrom evaluates a ValidCard$-style filter against the live
	// object id from source's perspective with you as its controller.
	MatchesSpecFrom(spec string, id state.ObjID, you state.PlayerID, source state.ObjID) bool
	// CastProvenanceAdmitsPending splits the cast-provenance qualifiers out
	// of spec and evaluates them for objID, pending-cast aware: the rest of
	// the spec, and whether the provenance admits.
	CastProvenanceAdmitsPending(spec string, objID state.ObjID, you state.PlayerID) (string, bool)

	// CostBlocked reports whether a static forbids sacrificing (BlockSacrifice:
	// a CantSacrifice ForCost$/ValidCause$ line) or exiling (BlockExile: a
	// CantExile ForCost$ line) id to pay a cost for cause.
	CostBlocked(op CostBlock, id state.ObjID, cause CostCause) bool

	// AddsCounterGrant resolves one consumed AddsCounters$ rider batch into
	// the grant a cast records (used units of it), ok=false to drop it.
	AddsCounterGrant(r state.ManaRestriction, used int32) (state.ManaAddsCounterGrant, bool)
	// Capture is the engine's transient spend capture a cast payment fills.
	Capture() Capture

	// ConfiguredCost is the engine's compiled-text sidecar entry for a cost
	// text (its frozen parse and facts), nil for a text outside the
	// configured set; CompiledCostOf, CostRef and ParseCostOf build on it.
	ConfiguredCost(raw string) *CompiledCost
	// Chars is the characteristics layer's read interface: derived
	// characteristics and the activation gates every ability shares
	// (lasagna spec §9.2).
	Chars() chars.Reader
	// Eval is the evaluation seam: everything that needs the engine as an
	// effects.Host (lasagna spec §9.2).
	Eval() Eval
}

// Capture points at the engine's transient spend-capture fields that a
// restricted-mana spend fills for the cast flow to fold in later: the spell
// a consumed AddsNoCounter$ batch protects, the consumed batches' producing
// sources (TriggersWhenSpent$) and the consumed AddsCounters$ grants.
type Capture struct {
	NoCounter    *state.ObjID
	Sources      *[]state.ObjID
	AddsCounters *[]state.ManaAddsCounterGrant
}

// CostBlock names the cost action Engine.CostBlocked asks about.
type CostBlock uint8

const (
	// BlockSacrifice asks whether id may not be sacrificed for the cost.
	BlockSacrifice CostBlock = iota
	// BlockExile asks whether id may not be exiled for the cost.
	BlockExile
)
