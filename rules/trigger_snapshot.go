package rules

import (
	"github.com/adams-shaun/gorge/state"
)

// triggerSnapshot is immutable look-back state. Parked replacement choices
// may retain it across intent/Clone boundaries; each matching walk constructs
// its own Engine scratch caches, never mutating or sharing the snapshot's.
type triggerSnapshot struct {
	game       *state.Game
	continuous []ContinuousEffect
	// retained marks a snapshot a parked record holds (retainTriggerBefore):
	// it outlives the window that took it, so its window never recycles its
	// arena (trigger_snapshot_pool.go). Set only while the snapshot is still
	// private to the engine that took it, so a snapshot Clone shares is
	// never written.
	retained bool
	// noLookBack: the board was proven inert for the look-back walk when the
	// window opened (lookBackNoopBoard); see noLookBackSnapshot.
	noLookBack bool
	// staticOwner/staticSeq record the taking engine's static memo when it
	// was current and quiet for exactly this board (lookback_static.go):
	// the observer adopts that engine's memo while it is still that build.
	staticOwner *Engine
	staticSeq   uint64
}
