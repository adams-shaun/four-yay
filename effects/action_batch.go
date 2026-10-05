package effects

// action_batch.go is the one shared open/close helper for the engine's
// "one or more" action bracket (effects.Cardflow's effMill and combatfx's
// effTapAll/effUntapAll). A bracket groups the events of ONE action so the
// batch trigger modes (Mode$ MilledAll, TapAll, UntapAll) fire once for the
// whole action rather than once per event. The bracket itself lives in the
// rules engine (rules/trigger_match.go's openMillBatch/closeMillBatch); this
// helper reaches it through an optional interface so a host double without it
// still runs, just without the batch.

// actionBatcher is the engine's action bracket. It is deliberately optional
// (not part of effects.Host): a host double simply fires the batch modes
// per event.
type actionBatcher interface {
	BeginActionBatch()
	EndActionBatch()
}

// beginActionBatch opens one action bracket and returns the function that
// closes it. A host that does not implement the bracket returns a no-op
// close. Callers use it as `defer beginActionBatch(h)()`.
func beginActionBatch(h Host) func() {
	if b, ok := h.(actionBatcher); ok {
		b.BeginActionBatch()
		return b.EndActionBatch
	}
	return func() {}
}
