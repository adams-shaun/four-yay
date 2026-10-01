package rules

// Hypothetical clones: the throwaway engines a pure read builds to try a
// line of play (potentialWitnessReaches, PotentialPlayScript's search). Each
// such clone dies before the read returns, so its storage -- the log, object
// arena and memo arrays CloneInto draws from a Spare -- is recycled through
// a small stack of Spares the reading engine owns instead of being allocated
// and collected per clone. CloneInto's contract (TestCloneIntoIsInvisible)
// makes a recycled clone identical to a fresh Clone in everything a caller
// can observe.

// hypSparePool is an engine's stack of spent hypothetical clones' Spares.
// owner guards it against a by-value Engine copy (entryPreview's preview),
// which must never pop or push the owner's stack.
type hypSparePool struct {
	owner  *Engine
	spares []Spare
}

// hypPool is e's own pool, created on first use.
func (e *Engine) hypPool() *hypSparePool {
	if e.hypSpares == nil || e.hypSpares.owner != e {
		e.hypSpares = &hypSparePool{owner: e}
	}
	return e.hypSpares
}

// hypClone clones c (e itself or one of e's hypothetical clones) from e's
// pool. The clone must be handed back with hypRelease once it, and every
// clone made from it, is dead.
func (e *Engine) hypClone(c *Engine) *Engine {
	sp := e.hypTake()
	return c.CloneInto(&sp)
}

// hypTake pops a Spare from e's pool (the zero Spare when it is empty).
func (e *Engine) hypTake() Spare {
	pl := e.hypPool()
	n := len(pl.spares)
	if n == 0 {
		return Spare{}
	}
	sp := pl.spares[n-1]
	pl.spares[n-1] = Spare{}
	pl.spares = pl.spares[:n-1]
	return sp
}

// hypRelease releases a dead hypothetical clone into e's pool.
func (e *Engine) hypRelease(c *Engine) { e.hypPut(c.Release()) }

// hypPut returns a Spare to e's pool.
func (e *Engine) hypPut(sp Spare) {
	pl := e.hypPool()
	pl.spares = append(pl.spares, sp)
}
