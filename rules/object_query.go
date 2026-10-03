package rules

import "github.com/adams-shaun/gorge/state"

// object_query.go holds the engine-wide object queries every subsystem
// reads: who controls an object (controllerOf, with the recurring-Effect
// matching overlay it must honour) and the one deterministic walk over every
// object in the game (forEachObject). They used to live in trigger_match.go,
// which made every caller -- casting, combat, layers, replacements, effects
// hosting -- depend on the trigger matcher's file; they are not trigger
// logic (lasagna spec W5 step E2), and moving them out is what lets the
// trigger matcher become its own package (E3).

// objectWalkZones is the deterministic per-seat zone order forEachObject and
// the trigger walk visit. It is the historical library..stack order with the
// command zone appended after the stack, matching ZCommand's own
// append-after-stack definition (state/ids.go) while touching no existing
// zone ordinal. ZCeased is deliberately absent: it is an inert tombstone with
// no membership list, not a game zone. ZSideboard and ZPlanarDeck are private
// zones outside the ordinary walk.
var objectWalkZones = [...]state.Zone{
	state.ZLibrary, state.ZHand, state.ZBattlefield,
	state.ZGraveyard, state.ZExile, state.ZStack, state.ZCommand,
}

// forEachObject walks every object currently in the game exactly once, in a
// fixed, deterministic order: living seats in ascending order from seat 0,
// then zone in objectWalkZones' declared order (library, hand, battlefield,
// graveyard, exile, stack, command), then position within that zone's slice.
// checkTriggers and applyReplacements both need this same walk for their own
// discovery to be deterministic, so it is factored out here rather than
// duplicated. Game.Zone ignores its player argument for ZStack (the stack is
// shared across controllers, not per-seat), so that zone is visited only
// once, on the first living seat, rather than once per living player. The
// command zone joined the walk so a printed trigger that functions from it
// (Sidar Jabari of Zhalfir's Eminence, and the three other Eminence cards) is
// scanned at all; printed replacements reached through this walk are held to
// their declared zone by commandReplZoneAdmits.
//
// Memory: the live zone slice is copied into e.foreachBuf (engine.go) before
// the walk, because fn can move objects between zones (a trigger match
// putting something on the stack), so iterating the live, mutating slice
// would be a bug. The copy is kept and grown with append(buf[:0], zone...),
// so it settles at the size of the largest zone seen and stops allocating;
// a fresh []state.ObjID allocation per zone per event used to be this
// package's single largest allocation site (Task A2). fn is NEVER called
// after the zone it is walking mutates.
//
// Re-entry safety: each rendering passes its snapshot buffer separately --
// the depth-0 call (from checkTriggers or applyReplacements, always) reuses
// e.foreachBuf; a re-entrant call (fn reaching forEachObject again, directly
// or through emit -> checkTriggers) takes a fresh local buffer instead, so
// the inner walk can never overwrite the outer walk's snapshot mid-range.
// That re-entry is not reachable today -- the only two callers' fns are
// checkTriggers' and applyReplacements', neither of which calls emit or
// forEachObject -- but the guard makes the shape safe if one ever does,
// which is why this file does not rely on "re-entry cannot happen" to keep a
// single shared buffer correct.
func (e *Engine) forEachObject(fn func(id state.ObjID)) {
	e.foreachDepth++
	defer func() { e.foreachDepth-- }()
	buf := e.foreachBuf
	if e.foreachDepth > 1 {
		// Re-entrant: own a private snapshot, never the shared field.
		buf = nil
	}
	for si, p := range e.G.AliveFrom(0) {
		for _, z := range objectWalkZones {
			if z == state.ZStack && si != 0 {
				continue
			}
			// Reassign (not append inline) so the grown snapshot is carried into
			// e.foreachBuf: buf[:0] then append-in-place reuses the backing array
			// from the previous zone / previous call, settling at the largest
			// zone and stopping allocation. Walking the returned slice, not a
			// throwaway append expression, keeps the range over the persisted
			// snapshot rather than a discarded temporary.
			buf = append(buf[:0], e.G.Zone(z, p)...)
			for _, id := range buf {
				fn(id)
			}
		}
	}
	if e.foreachDepth <= 1 {
		// Keep the grown buffer on the Engine for the next depth-0 walk; a
		// re-entrant call's private buffer is discarded on return.
		e.foreachBuf = buf
	}
}

// effectMatchControllerFor reports the recurring-Effect matching overlay's
// virtual controller for id. The overlay is a read-only matcher scope that
// makes a registering Effect's trigger read the player the Effect belongs to
// -- its registration owner -- rather than the controller of the creating
// card (see checkEventDelayedTriggers). It is true only for the source id the
// overlay was armed for; every other id takes the ordinary state.Object read.
//
// This is the ONE home for that check: every source-controller read must go
// through it (via controllerOf, or directly where an LKI look-back would
// otherwise clobber the overlay), so the next consumer cannot re-derive it
// wrong.
func (e *Engine) effectMatchControllerFor(id state.ObjID) (state.PlayerID, bool) {
	if e.effectMatchOverride && id == e.effectMatchSource {
		return e.effectMatchController, true
	}
	return 0, false
}

// controllerOf is a nil-safe Object.Controller read: a nonexistent ObjID
// (stale data, a malformed trigger source) degrades to seat 0 rather than
// panicking. A recurring-Effect registration's source resolves the Effect's
// owner instead of the creating card's controller.
func (e *Engine) controllerOf(id state.ObjID) state.PlayerID {
	if c, ok := e.effectMatchControllerFor(id); ok {
		return c
	}
	if o := e.G.Obj(id); o != nil {
		return o.Controller
	}
	return 0
}
