package resolve

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
)

// Board is the kernel's whole view of the engine. Package rules implements it
// over its Engine (a pointer conversion, no allocation). No effects.Ctx or
// SpecContext crosses it: what a resolution needs, the engine computes on its
// own side.
//
// Its size is a shrink-only archtest ratchet
// (internal/archtest/layering_resolve_test.go): fold a new need into an
// existing method rather than adding one.
type Board interface {
	// Log is the engine's event and intent log.
	Log() *events.Log
	// Pending is the decision posed to a seat, nil when none is.
	Pending() *decision.Decision
	// Busy reports a legacy resolution suspension in flight (a resume
	// point, or an unless-cost, cumulative-upkeep, trigger-cost or
	// off-stack mana window): the kernel never nests a tape run in one.
	Busy() bool
	// StartsResolution reports whether answering d with in is the last pass
	// over a non-empty stack: the pass whose handler resolves the top object.
	StartsResolution(d *decision.Decision, in decision.Intent) bool
	// MayAsk is the ask-free predicate over the object about to resolve:
	// false means its resolution provably poses no decision, so the kernel
	// skips the checkpoint. true is always safe.
	MayAsk() bool

	// Checkpoint clones the engine (S0) into recycled storage. It is called
	// only at an intent boundary.
	Checkpoint() Snapshot
	// Drop recycles a checkpoint no clone can share (its resolution never
	// posed a tape ask).
	Drop(s Snapshot)
	// Restore makes the engine a copy of cp.S0 in place, keeping its Game
	// and Log identity and the kernel's own state, rewinds the log to S0
	// with a verify window over its first evEnd events
	// (events.Log.RewindTo), applies cp's hypothetical-world splice, and
	// parks the harness observers so the recorded prefix runs unobserved.
	// Intents are the kernel's.
	Restore(cp *Checkpoint, evEnd int)
	// Observe re-attaches the observers Restore parked (no-op when none).
	Observe()

	// Validate is Submit's validation of in against the posed d: a pure
	// read.
	Validate(d *decision.Decision, in decision.Intent) error
	// Commit is Submit past validation: it records in, emits DecisionMade
	// and runs the answer's handler and Submit's idle tail.
	Commit(d *decision.Decision, in decision.Intent)
	// Submit is the engine's whole Submit (a re-executed pass; the legacy
	// replay of a tape after an abort).
	Submit(in decision.Intent) error
	// Pose poses d through the engine's one ask choke point (the same
	// DecisionAsk event a legacy ask emits).
	Pose(d *decision.Decision)
	// Record answers the posed d with in exactly as a Submit would record
	// it -- the intent, DecisionMade, and the decision kind's own answer
	// record (a mid-resolution KModes answer's ModeChosen marker) -- clears
	// the pending decision and returns the chosen options.
	Record(d *decision.Decision, in decision.Intent) []decision.Option
	// Emit applies and logs one event (a hypothetical world's injected
	// redeal events, re-applied at its fork point).
	Emit(ev events.Event)
}

// Snapshot is a checkpoint engine, opaque to the kernel: package rules
// passes its own *Engine.
type Snapshot interface{}

// Answerer answers a decision posed inside a resolution synchronously.
// reject is the previous answer's validation error (nil on the first call);
// ok false gives up, which fails the game loudly (a Divergence).
type Answerer func(d *decision.Decision, reject error) (in decision.Intent, ok bool)
