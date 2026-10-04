package rules

import (
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// host_emit.go is the engine's implementation of effects.HostEmit, the emit
// role of effects.Host (rules-engine refactor spec W1d): every event an effect
// proposes enters the engine through one of these.

func (e *Engine) Emit(ev events.Event) { e.emit(ev) }

// EmitScryRecord is effects.Host's completed-scry-record emit (task
// scrybottom): the stand-in completion in effects' effLookAndArrange goes
// through emitScryRecord, the exact site handleArrange uses, so a scry that
// finishes without an answerable KArrange (an empty library, ScryNum$ 0, a
// no-host run) still records its zero-card bottom pile outside the
// replacement pass.
func (e *Engine) EmitScryRecord(ev events.Event) { e.emitScryRecord(ev) }

// EmitTokenCreate emits a token-creation event (TokenCreate, Encore's
// CardToken copy, or the battlefield MoveZone of a CopyToken mint) and
// returns every token whose battlefield entry the emit actually completed --
// the ids publishTokenEntry (rules/token_rest.go) published, never a mint
// still parked behind an entry-counter order. A CreateToken replacement may rewrite one would-be token
// into several mints (Divine Visitation, Doubling Season, Xorn);
// effects/token.go consults this return so its per-token riders land on
// EVERY mint, not just the first. The sink is a stack: a nested token
// creation during this emit saves and restores it, so the outer call returns
// only its own plan's mints.
func (e *Engine) EmitTokenCreate(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.tokenMintSink
	e.tokenMintSink = &ids
	e.emit(ev)
	e.tokenMintSink = saved
	return ids
}

// EmitStackCopy emits a StackCopy event and returns the object it actually
// minted. The copy object is created inside events.Apply's StackCopy fold, so
// effects/copy.go's RememberCopies$ rider (Forge's card.addRemembered) cannot
// see its id from Emit; this method's sink records the pre-fold NextID the
// fold's AddObject assigns. The empty return is the fold's early breaks (no
// source, source already left the stack) -- a proposed copy that minted
// nothing, which must not be remembered. Same stack discipline as
// EmitTokenCreate, so a nested stack copy cannot leak its mint to the outer
// caller.
func (e *Engine) EmitStackCopy(ev events.Event) []state.ObjID {
	var ids []state.ObjID
	saved := e.stackCopyMintSink
	e.stackCopyMintSink = &ids
	e.emit(ev)
	e.stackCopyMintSink = saved
	return ids
}

func (e *Engine) EmitDamage(ev events.Event) events.Event { return e.emit(ev) }

// EmitTap satisfies effects.Host's EmitTap: see emitTap.
func (e *Engine) EmitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	e.emitTap(obj, tapper, entering)
}

// emitTap emits the plain Tap event for obj while its provenance -- who tapped
// it, and whether it is only being given its entry state -- is visible to the
// Taps/TapsForMana matcher and to trigger referents. The event payload is the
// same one every Tap producer emitted before, so no chain head moves for a
// game without such a trigger. The previous context is restored rather than
// zeroed, so a Tap emitted from inside another Tap's trigger matching cannot
// clobber the outer one.
func (e *Engine) emitTap(obj state.ObjID, tapper state.PlayerID, entering bool) {
	savedObj, savedPlayer, savedEntering := e.tapObj, e.tapPlayer, e.tapEntering
	e.tapObj, e.tapPlayer, e.tapEntering = obj, tapper, entering
	e.emit(events.Event{Kind: events.Tap, Obj: obj})
	e.tapObj, e.tapPlayer, e.tapEntering = savedObj, savedPlayer, savedEntering
}
