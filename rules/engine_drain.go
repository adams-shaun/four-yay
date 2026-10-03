package rules

// engineDrain groups the Engine's trigger-drain queue cursor and the
// replacement re-entrancy guards. It is embedded by value in Engine
// (rules/engine_struct.go), so every field keeps its documented contract
// comment and every existing e.<field> access keeps compiling unchanged
// through Go's field promotion. Clone's per-field copy classes
// (rules/clone.go) are unchanged by the move.
type engineDrain struct {
	// pendingTriggers holds matched triggers not yet placed on the stack.
	// checkTriggers appends; putTriggersOnStack drains. Task 20 (trigger.go).
	pendingTriggers []pendingTrigger `clone:"deep,pool=pending,release=clear"`
	// trigQueueStale bounds the prefix of pendingTriggers' backing array that
	// may hold entries a shrink left behind len (noteTrigShrink records each
	// shrink's pre-shrink length): what the drained queue must zero so
	// nothing stale stays pinned. Scratch hygiene, never game state.
	trigQueueStale int `clone:"reset"`

	// orderedTriggers is how many LEADING entries of pendingTriggers have
	// already had their order settled by an answered KTriggerOrder decision
	// (or, for a lone trigger, by there being nothing to decide). It is the
	// whole of Task 27's resumable-drain state, and it exists because the
	// queue can grow while a controller is being asked: Submit runs handle,
	// then checkStateBased, then Advance, and checkStateBased (sba.go) is a
	// fixed-point loop whose PlayerLost/MoveZone/GameOver emits each run
	// checkTriggers. Appends land at the END; decisions are always about the
	// FRONT; and while this is non-zero putTriggersOnStack does not re-sort,
	// so a trigger that arrives mid-drain can neither be shuffled into a
	// group the player has already ordered nor make them order the same
	// triggers twice. Zero whenever pendingTriggers is empty.
	orderedTriggers int `clone:"deep"`
	// applyingReplacement guards re-entrancy for the replaced event. Fresh
	// counter placements emitted by its body still receive their own
	// AddCounter replacement pass (unless already folded below).
	applyingReplacement bool `clone:"deep"`
	// counterReplacementFold marks the already-rewritten event's final emit;
	// new counter events from a replacement body still take their own pass.
	counterReplacementFold bool `clone:"reset"`
}
