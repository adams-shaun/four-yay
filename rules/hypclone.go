package rules

import (
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
	// opts is a stack of cleared option lists the temporary offer walks
	// (legalActionsWalkTemp) build into.
	opts [][]decision.Option
	// pm is PotentialMana's working storage (potential.go), taken for each
	// call (empty while one runs).
	pm potentialManaScratch
	// planSearch is the payment-plan search's working storage
	// (payment_plan_search.go).
	planSearch paymentPlanSearchScratch
	// zoneEntry backs the engine's zone-entry index (payment_zone_entry.go).
	zoneEntry []zoneEntryRec
	// ids is a stack of id lists read-only walks borrow (idsBorrow).
	ids [][]state.ObjID
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
	// A pooled log array sized for an earlier, shorter history would be
	// skipped by CloneInto (it needs the history plus its fork slack) and
	// the clone would regrow a fresh one at only an eighth of slack; the
	// live log keeps growing all game, so that regrow would recur every few
	// decisions. Replace a short array once with half the history again of
	// headroom. Capacity only: CloneInto overwrites every slot it hands out.
	if need := len(c.L.Events) + hypLogSlack; cap(sp.events) < need {
		sp.events = make([]events.Event, 0, need+len(c.L.Events)/2)
	}
	// Likewise the object arena (Game.CloneInto needs the objects plus its
	// small minting headroom; a fresh array is zero, as a released one is).
	if need := len(c.G.Objs) + hypObjSlack; cap(sp.objs) < need {
		sp.objs = make([]state.Object, 0, need+len(c.G.Objs)/2)
	}
	return c.CloneInto(&sp)
}

// hypLogSlack is the appends a hypothetical clone's log array holds past its
// parent's history; it is at least events' fork slack (256), so CloneInto
// always takes the array.
const hypLogSlack = 512

// hypObjSlack is the objects a hypothetical clone's arena holds past its
// parent's; at least state's clone headroom (8).
const hypObjSlack = 32

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

// optBorrow pops a cleared option list from e's pool (nil when empty).
func (e *Engine) optBorrow() []decision.Option {
	pl := e.hypPool()
	n := len(pl.opts)
	if n == 0 {
		return nil
	}
	b := pl.opts[n-1]
	pl.opts[n-1] = nil
	pl.opts = pl.opts[:n-1]
	return b
}

// optRelease clears a list legalActionsWalkTemp returned (dropping its
// labels, slices and grants) and pushes it back onto e's pool.
func (e *Engine) optRelease(b []decision.Option) {
	if cap(b) == 0 {
		return
	}
	clear(b)
	pl := e.hypPool()
	pl.opts = append(pl.opts, b[:0])
}

// releaseHypPool detaches e's own pool for a Spare (nil when e has none).
// Its Spares and scratch hold only cleared or call-local storage.
func (e *Engine) releaseHypPool() *hypSparePool {
	pl := e.hypSpares
	e.hypSpares = nil
	if pl == nil || pl.owner != e {
		return nil
	}
	pl.owner = nil
	return pl
}

// adoptHypPool makes a Spare's pool e's own.
func (e *Engine) adoptHypPool(pl *hypSparePool) {
	if pl == nil {
		return
	}
	pl.owner = e
	e.hypSpares = pl
}

// idsBorrow pops an empty id list from e's pool (nil when empty); the caller
// appends into it, only ranges the result, and hands it back with
// idsRelease. Nested borrowers get distinct lists.
func (e *Engine) idsBorrow() []state.ObjID {
	pl := e.hypPool()
	n := len(pl.ids)
	if n == 0 {
		return nil
	}
	b := pl.ids[n-1]
	pl.ids[n-1] = nil
	pl.ids = pl.ids[:n-1]
	return b[:0]
}

// idsRelease returns a borrowed id list to e's pool.
func (e *Engine) idsRelease(b []state.ObjID) {
	if cap(b) == 0 {
		return
	}
	pl := e.hypPool()
	pl.ids = append(pl.ids, b[:0])
}
