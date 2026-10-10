package pay

import (
	"github.com/adams-shaun/gorge/decision"
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
	// Session is the engine-owned payment state (the ring's fields).
	Session() *Session
	// SearchScratch is the plan search's reusable working storage.
	SearchScratch() *SearchScratch
	// Rand is the engine's deterministic random draw in [0, n) (a
	// Discard<N/Random> cost part's pick).
	Rand(n int) int

	// Ask is the flow seam (lasagna spec §9.2, E7 flow slice): it marks
	// flow as the engine's pending choice and poses d. Under the resolution
	// kernel the answer is served from the tape and the flow's answer
	// handler runs in place before Ask returns; otherwise d is posed and
	// the handler runs when the seat answers. Either way the caller returns
	// right after Ask: the payment's state (the Session) is what the handler
	// continues from.
	Ask(flow AskFlow, d *decision.Decision)
	// Batch opens (open) or closes one action bracket of kind: the emissions
	// between are one action for the batch-observing triggers (CR 701.8's
	// "discard two cards" is one discard action).
	Batch(kind BatchKind, open bool)
}

// AskFlow names the payment flow an Engine.Ask belongs to: the engine maps it
// to its own pending-choice marker and answer handler.
type AskFlow uint8

const (
	// AskUnlessCost asks for the payer-chosen objects of an unless cost's
	// next choice-bearing component.
	AskUnlessCost AskFlow = iota + 1
	// AskUnlessMana asks for the next mana source (or Done) of an unless
	// cost's CR 601.2g window.
	AskUnlessMana
	// AskManaTap .. AskManaUntap ask for a mana ability's cost election
	// (ManaCostTapStage, ManaCostChoiceStages).
	AskManaTap
	AskManaSacrifice
	AskManaDiscard
	AskManaExile
	AskManaForage
	AskManaUntap
	// AskManaSubCounter asks for a mana ability's announced SubCounter X or
	// one removal pick (ManaCostSubCounterStage).
	AskManaSubCounter
	// AskManaEvidence asks for a mana ability's CollectEvidence<N> election
	// (manaCostEvidenceStage).
	AskManaEvidence
	// AskCast asks one of the cast's payment questions (ConvokeAsk, ManaAsk,
	// ManaConvertAsk); the engine's cast flow answers it.
	AskCast
)

// BatchKind names the action bracket an Engine.Batch opens or closes.
type BatchKind uint8

const (
	// BatchDiscard brackets one discard action (Mode$ DiscardedAll).
	BatchDiscard BatchKind = iota
	// BatchMill brackets one mill action (Mode$ MilledAll).
	BatchMill
	// BatchTap brackets one tapping action (Mode$ TapAll): the tap cost parts
	// of one payment tap their elected permanents as ONE action, so the
	// aggregate trigger fires once for the whole cost (task
	// cli-20261005T075020Z-05241a06). It rides the shared action bracket the
	// rules engine already exposes to effects.
	BatchTap
)

// CostBlock names the cost action Engine.CostBlocked asks about.
type CostBlock uint8

const (
	// BlockSacrifice asks whether id may not be sacrificed for the cost.
	BlockSacrifice CostBlock = iota
	// BlockExile asks whether id may not be exiled for the cost.
	BlockExile
)
