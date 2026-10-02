package rules

import "github.com/adams-shaun/gorge/state"

// Trigger-window snapshot recycling.
//
// A leaves-the-battlefield look-back window (emit's single departure, and
// the SBA batches in sba.go) snapshots the whole game (snapshotTriggerBoard,
// CR 603.10a) and drops the snapshot when the window closes. The object
// arena is almost all of that snapshot's bytes, and the search plays
// thousands of departures per decision, so a window that closes with its
// snapshot unretained hands the arena back to the engine's pool for the next
// window's snapshot instead of leaving it to the collector.
//
// Retention is the whole safety argument. A snapshot outlives its window
// only by being stored in a parked record (a replacement choice, a resume
// frame, a commander-zone move, an entry stage), and every such store goes
// through retainTriggerBefore, which marks it; TestTriggerBeforeReadsAreClassified
// holds every read of e.triggerBefore in the package to that rule. Pointers
// into the arena itself do not escape either: the one look-back object a
// matched trigger keeps is copied out (checkTriggers), and the observer
// Engine that reads the arena lives for one checkTriggers call. Tests run
// with triggerSnapshotPoison on (trigger_snapshot_pool_test.go), which
// scribbles over every recycled arena, so a stale reader diverges the
// goldens instead of silently reading the next window's board.

// snapshotPool is one engine's free list of snapshot object arenas.
type snapshotPool struct {
	owner *Engine
	objs  [][]state.Object
}

// snapshotPoolKeep bounds the free list: windows nest only a few deep.
const snapshotPoolKeep = 4

// triggerSnapshotPoison is a test hook: when set, a recycled arena is
// overwritten with unusable objects instead of cleared, so any read through
// a pointer that outlived its window reads garbage and fails loudly.
var triggerSnapshotPoison bool

// pool is e's own snapshot pool, created on first use; nil for an engine
// that does not own the pool it carries (a by-value copy of another
// engine), which then neither takes from nor returns to it.
func (e *Engine) pool() *snapshotPool {
	if e.snapPool == nil {
		e.snapPool = &snapshotPool{owner: e}
	}
	if e.snapPool.owner != e {
		return nil
	}
	return e.snapPool
}

// takeSnapshotObjs pops a recycled arena for the next snapshot (nil when the
// pool is empty or foreign; Game.CloneInto then allocates exactly as Clone
// does, and drops an arena too small for this board).
func (e *Engine) takeSnapshotObjs() []state.Object {
	p := e.pool()
	if p == nil || len(p.objs) == 0 {
		return nil
	}
	n := len(p.objs) - 1
	objs := p.objs[n]
	p.objs[n] = nil
	p.objs = p.objs[:n]
	return objs
}

// retainTriggerBefore is the ONE way a parked record takes the current
// window's snapshot: it marks the snapshot retained, so the window that
// took it never recycles its arena, and returns it.
func (e *Engine) retainTriggerBefore() *triggerSnapshot {
	if s := e.triggerBefore; s != nil && !s.retained {
		s.retained = true
	}
	return e.triggerBefore
}

// closeTriggerWindow restores the window's outer snapshot and recycles the
// window's own, unless a record retained it. Deferred by every window.
func (e *Engine) closeTriggerWindow(own, outer *triggerSnapshot) {
	e.triggerBefore = outer
	if own == nil || own.retained || own == outer || own.game == nil {
		return
	}
	p := e.pool()
	if p == nil || len(p.objs) >= snapshotPoolKeep {
		return
	}
	objs := own.game.Objs[:cap(own.game.Objs)]
	own.game.Objs = nil
	own.game = nil
	if triggerSnapshotPoison {
		poisonObjects(objs)
	} else {
		clear(objs)
	}
	p.objs = append(p.objs, objs)
}

// openBatchWindow parks the pre-departure board for an effect's simultaneous
// departure batch (BatchDepartures, engine_damage.go) unless a window is
// already open -- an outer state-based batch or parked replacement keeps
// winning, and nested batches share the outermost board.
func (e *Engine) openBatchWindow() {
	if e.batchWindowDepth == 0 && e.triggerBefore == nil {
		e.batchWindow = e.snapshotTriggerBoard()
		e.triggerBefore = e.batchWindow
	}
	e.batchWindowDepth++
}

// closeBatchWindow ends one BatchDepartures call; the outermost one closes
// the window it opened. The effect loop is over, or suspended on an ask
// (which returns through the caller's defer), so no batch window outlives an
// intent boundary.
func (e *Engine) closeBatchWindow() {
	if e.batchWindowDepth > 0 {
		e.batchWindowDepth--
	}
	if e.batchWindowDepth != 0 || e.batchWindow == nil {
		return
	}
	own := e.batchWindow
	e.batchWindow = nil
	if e.triggerBefore == own {
		e.closeTriggerWindow(own, nil)
	}
}

// poisonObjects overwrites every slot with an object no correct reader can
// mistake for a real one: a foreign ID, an out-of-range controller and zone,
// and no card, so a stale pointer read fails a match, indexes out of range
// or dereferences nil.
func poisonObjects(objs []state.Object) {
	for i := range objs {
		objs[i] = state.Object{ID: state.ObjID(0xdeadbeef), Controller: 0xee, Owner: 0xee, Zone: state.Zone(0xee), Damage: 1 << 30, Tapped: true}
	}
}

// releaseSnapshotObjs hands the pool's arenas to a Spare (Engine.Release).
func (e *Engine) releaseSnapshotObjs() [][]state.Object {
	p := e.pool()
	if p == nil {
		return nil
	}
	objs := p.objs
	p.objs = nil
	return objs
}

// adoptSnapshotObjs seeds a new engine's pool from a Spare (cloneWith).
func (e *Engine) adoptSnapshotObjs(objs [][]state.Object) {
	if len(objs) == 0 {
		return
	}
	e.snapPool = &snapshotPool{owner: e, objs: objs}
}

// lookBackObserver hands checkTriggers the Engine struct its look-back
// observer is built in: e's own reusable one when it is free, else a fresh
// one. The caller overwrites the whole struct before use, so a reused
// observer carries nothing -- no cache, no buffer, no epoch -- from an
// earlier call, and nothing retains an observer past its call (it never
// emits, and matched triggers keep values, not the observer).
func (e *Engine) lookBackObserver() *Engine {
	if e.lookBackOwner != e || e.lookBack == nil {
		e.lookBack, e.lookBackOwner, e.lookBackBusy = &Engine{}, e, false
	}
	if e.lookBackBusy {
		return &Engine{}
	}
	e.lookBackBusy = true
	return e.lookBack
}

// releaseLookBackObserver ends a look-back observer's use, dropping its
// references so the scratch struct pins no snapshot between calls.
func (e *Engine) releaseLookBackObserver(o *Engine) {
	if o == e.lookBack && e.lookBackOwner == e {
		// The static memo's outer array is kept for the next observer's
		// build or seed (both overwrite it from index 0); its entries are
		// shallow effect values over shared card tables.
		buf := o.staticContinuous[:0]
		*o = Engine{}
		o.staticContinuous = buf
		e.lookBackBusy = false
	}
}

// noLookBackSnapshot is the window snapshot of a board no look-back walk can
// act on (lookBackNoopBoard): it copies nothing, and checkTriggers skips the
// look-back walk for it. It is shared by every engine and never written:
// retained is already set (retainTriggerBefore writes only a false flag) and
// closeTriggerWindow returns before touching a retained or arena-less
// snapshot.
var noLookBackSnapshot = &triggerSnapshot{retained: true, noLookBack: true}

// lookBackProvenNoop reports whether the current window's board was proven
// inert for the look-back walk when the window opened.
func (e *Engine) lookBackProvenNoop() bool {
	s := e.triggerBefore
	return s != nil && s.noLookBack
}

// lookBackStaticSnap reports the current window snapshot's recorded static
// memo owner and build (lookback_static.go); nil when there is none.
func (e *Engine) lookBackStaticSnap() (*Engine, uint64) {
	if s := e.triggerBefore; s != nil {
		return s.staticOwner, s.staticSeq
	}
	return nil, 0
}

// releaseEntryPreview ends an entry preview's use (entry_counters.go): its
// cloned object arena, taken from e's pool by entryPreview, is cleared (or
// poisoned under triggerSnapshotPoison) and pooled for the next snapshot or
// preview. A preview is private to the one call that built it: it is never
// stored, and nothing it produced points into its arena (matches name faces
// and repl lines of the shared card tables, choices carry event values).
func (e *Engine) releaseEntryPreview(preview *Engine) {
	if preview == nil || preview.G == nil {
		return
	}
	defer e.releasePreviewStruct(preview)
	p := e.pool()
	if p == nil || len(p.objs) >= snapshotPoolKeep {
		return
	}
	n := len(preview.G.Objs)
	objs := preview.G.Objs[:cap(preview.G.Objs)]
	preview.G.Objs = nil
	if triggerSnapshotPoison {
		poisonObjects(objs)
	} else {
		clear(objs[:n])
	}
	p.objs = append(p.objs, objs)
}

// previewStruct hands entryPreview the Engine struct its preview is built
// in: e's own reusable one when it is free, else a fresh one. The caller
// overwrites the whole struct.
func (e *Engine) previewStruct() *Engine {
	if e.previewOwner != e || e.preview == nil {
		e.preview, e.previewOwner, e.previewBusy = &Engine{}, e, false
	}
	if e.previewBusy {
		return &Engine{}
	}
	e.previewBusy = true
	return e.preview
}

// releasePreviewStruct zeroes e's reusable preview struct when p is it, so
// it pins nothing between entries, and frees it for the next preview.
func (e *Engine) releasePreviewStruct(p *Engine) {
	if p == e.preview && e.previewOwner == e {
		*p = Engine{}
		e.previewBusy = false
	}
}
