package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// inFlightDamageSource is the one reader for Damage-event provenance: the
// published override when a damage emitter set one, else the resolution/combat
// source e.damaging carries. Zero when neither is set (a Damage event with no
// recorded source -- emit's protection check treats zero as "never prevent",
// and damageMatches fails the ValidSource$ match).
func (e *Engine) inFlightDamageSource() state.ObjID {
	if e.dmgSrcOverride != 0 {
		return e.dmgSrcOverride
	}
	return e.damaging
}

// SetDamageSource implements effects.Host: publish the damage source for the
// Damage events the calling emitter is about to emit, returning the previous
// value so the emitter restores it. See dmgSrcOverride's field doc for the
// replay/clone discipline.
func (e *Engine) SetDamageSource(id state.ObjID) state.ObjID {
	prev := e.dmgSrcOverride
	e.dmgSrcOverride = id
	return prev
}

// inFlightCounterAdder is the one reader for AddCounter-replacement
// provenance: the player causing the counter placement currently in flight,
// and whether that attribution is known at all. A published override wins
// (the cost/turn-based sites, where no stack cause exists yet). Otherwise the
// controller of actionCause() -- the resolving spell or ability wrapper at
// the top of the stack -- is the adder: a resolving spell, activated ability
// or triggered-ability instruction IS the cause, and the counters it puts are
// put by that ability's controller (Vorinclex's "If YOU would put", Halving
// Season's "If an OPPONENT would put"). Third, a placement a replacement BODY
// makes (replacementBodyCounterAdder below) names the body source's
// controller: by the time the body's nested CounterChange emits, the entry
// move has already applied and the wrapper is off the stack, so the
// actionCause fallback cannot see it. Zero with ok=false when none of the
// three is available (an SBA or other bare placement): the AddCounter matcher
// then fails a ValidSource$ line closed rather than guessing an adder.
func (e *Engine) inFlightCounterAdder() (state.PlayerID, bool) {
	if e.counterAdder != 0 {
		return e.counterAdder - 1, true
	}
	if c := e.actionCause(); c != 0 {
		return e.controllerOf(c), true
	}
	if adder, ok := e.replacementBodyCounterAdder(); ok {
		return adder, true
	}
	return 0, false
}

// replacementBodyCounterAdder is the counter-adder provenance of a placement
// a replacement BODY makes. While a ReplaceWith$ body is resolving
// (applyingReplacement set, the final emit not yet folded), a
// CounterChange/PlayerCounterChange it emits is the body's own instruction:
// CR 614.5 -- the replacement does not use up its event, and the counter its
// instruction places is a NEW event whose cause is that replacement effect.
// The "who is putting these counters" role (ValidSource$) and the "an effect
// would put" wording (EffectOnly$) therefore read the body source's
// controller: for a K:etbCounter entry the source is the entering permanent,
// so its controller is the adder (Doubling Season doubles the entry counters;
// an opponent's Vorinclex halves them). The counterReplacementFold exclusion
// keeps the distinction that matters: the notification-only CounterChange
// records (foldEntryMove's EntryCounterNotice tail, and the AddCounter class's
// own fully-rewritten final emit) are already-settled echoes of a placement
// whose replacement pass has run, not body instructions, and inside them the
// source slot may name an unrelated outer replacement -- attributing the echo
// to it would double-count. Zero with ok=false outside a body or when the
// source has left the game.
func (e *Engine) replacementBodyCounterAdder() (state.PlayerID, bool) {
	if !e.applyingReplacement || e.counterReplacementFold || e.replacingSource == 0 {
		return 0, false
	}
	if e.G.Obj(e.replacingSource) == nil {
		return 0, false
	}
	return e.controllerOf(e.replacingSource), true
}

// counterAdderUnset is SetCounterAdder's opaque "no publication" token. It
// is state.PlayerID(255), a seat no game can hold, so a caller can round-trip
// the previous publication (including the absence of one) through the SAME
// method without a second restore call: seat 0 is a legitimate adder, so a
// bare 0 cannot double as the sentinel.
const counterAdderUnset = state.PlayerID(255)

// SetCounterAdder implements effects.Host: publish the player causing the
// CounterChange/PlayerCounterChange events the caller is about to emit, and
// return the previous publication -- a PlayerID, or counterAdderUnset when
// none was published -- for the caller to pass straight back to restore it
// (the SetDamageSource shape, extended with an explicit unset token so seat 0
// round-trips correctly). rules is the only publisher: effect-resolution
// sites are covered by inFlightCounterAdder's actionCause fallback, and only
// a cost or turn-based placement (which has no stack cause) needs an
// explicit publish.
func (e *Engine) SetCounterAdder(p state.PlayerID) state.PlayerID {
	prev := counterAdderUnset
	if e.counterAdder != 0 {
		prev = e.counterAdder - 1
	}
	if p == counterAdderUnset {
		e.counterAdder = 0
	} else {
		e.counterAdder = p + 1
	}
	return prev
}

// damageKeywordLKI is the derived damage-relevant keyword set of one source,
// snapshotted before it leaves the battlefield. CR 113.7a reads the source's
// last known characteristics for the whole damage rider: the life gain
// (CR 702.15a), the counter form (CR 702.90b) and the deadly mark
// (CR 702.2b) all answer off the same pre-departure state.
type damageKeywordLKI struct {
	lifelink   bool
	infect     bool
	wither     bool
	deathtouch bool
}

func (e *Engine) damageKeywordsOf(id state.ObjID) damageKeywordLKI {
	return damageKeywordLKI{
		lifelink:   e.hasKeywordH(id, kwhLifelink),
		infect:     e.hasKeywordH(id, kwhInfect),
		wither:     e.hasKeywordH(id, kwhWither),
		deathtouch: e.hasKeywordH(id, kwhDeathtouch),
	}
}

// BatchDepartures implements effects.Host: snapshot the derived damage
// keywords of every object the caller is about to move in one destruction
// batch, so each member's departure capture reads the pre-batch state no
// matter where it sits in battlefield order. See batchDamageKeywords' field
// doc for the consumption discipline.
func (e *Engine) BatchDepartures(ids []state.ObjID) {
	e.batchDamageKeywords = make(map[state.ObjID]damageKeywordLKI, len(ids))
	for _, id := range ids {
		e.batchDamageKeywords[id] = e.damageKeywordsOf(id)
	}
}

// EndBatchDepartures closes a destruction/sacrifice batch even if one of its
// proposed moves was prevented or replaced. Without this explicit boundary,
// that survivor's pre-batch LKI could be consumed by an unrelated later move.
func (e *Engine) EndBatchDepartures() { e.batchDamageKeywords = nil }

// captureSourceLifelinkLKI preserves CR 608.2h's pre-departure derived
// lifelink state for every independent ability of the leaving permanent that
// already exists on the stack or in the pending-trigger queue. It is called
// immediately before every rules-layer events.Emit path that can fold a real
// MoveZone, including Updated replacement paths that deliberately bypass
// Engine.emit to avoid matching the same replacement twice. Walking ordered
// slices rather than a map keeps this bookkeeping incapable of changing event
// order. A self-sacrifice activation is not minted until after its departure;
// payCast captures that one sibling before paying the cost.
func (e *Engine) captureSourceLifelinkLKI(ev events.Event) (bool, damageKeywordLKI, state.PlayerID) {
	if ev.Kind != events.MoveZone || ev.From != state.ZBattlefield ||
		ev.To == state.ZBattlefield {
		return false, damageKeywordLKI{}, 0
	}
	// A destruction batch's own pre-state wins (rules.Engine.BatchDepartures,
	// effects.Host): a later batch member must read the lifelink state from
	// immediately before the FIRST departure (CR 603.10a/702.15c -- the
	// destroy-all over a lifelink-granting Equipment and its bearer), not
	// the live layers an earlier member's departure already stripped. The
	// entry is consumed here; BatchDepartures rebuilds the map on its next
	// call, so a straggler for an object that never left cannot outlive one
	// effect call.
	kw, batched := e.batchDamageKeywords[ev.Obj]
	if batched {
		delete(e.batchDamageKeywords, ev.Obj)
	} else {
		kw = e.damageKeywordsOf(ev.Obj)
	}
	controller := e.G.Obj(ev.Obj).Controller
	for _, id := range e.G.Stack {
		if o := e.G.Obj(id); o != nil && o.Ability != nil && o.Source == ev.Obj {
			if e.sourceLifelinkLKI == nil {
				e.sourceLifelinkLKI = make(map[state.ObjID]bool)
			}
			if e.sourceControllerLKI == nil {
				e.sourceControllerLKI = make(map[state.ObjID]state.PlayerID)
			}
			e.sourceLifelinkLKI[id] = kw.lifelink
			e.sourceControllerLKI[id] = controller
		}
		e.captureNamedDamageSourceLKI(id, ev.Obj, kw, controller)
	}
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == ev.Obj {
			e.pendingTriggers[i].Ctx.SourceLifelinkLKI = kw.lifelink
			e.pendingTriggers[i].Ctx.SourceLifelinkLKIValid = true
			e.pendingTriggers[i].Ctx.SourceControllerLKI = controller
			e.pendingTriggers[i].Ctx.SourceControllerLKIValid = true
		}
		e.capturePendingNamedDamageSourceLKI(&e.pendingTriggers[i].Ctx, ev.Obj, kw, controller)
	}
	return true, kw, controller
}

// finishSourceLifelinkLKI attaches the same pre-departure snapshot to a
// dies/leaves trigger that the event itself just queued. Such a trigger did not
// exist during captureSourceLifelinkLKI's pre-event walk.
func (e *Engine) finishSourceLifelinkLKI(ev events.Event, departing bool, kw damageKeywordLKI, controller state.PlayerID) {
	if !departing {
		return
	}
	for i := range e.pendingTriggers {
		if e.pendingTriggers[i].Source == ev.Obj {
			e.pendingTriggers[i].Ctx.SourceLifelinkLKI = kw.lifelink
			e.pendingTriggers[i].Ctx.SourceLifelinkLKIValid = true
			e.pendingTriggers[i].Ctx.SourceControllerLKI = controller
			e.pendingTriggers[i].Ctx.SourceControllerLKIValid = true
		}
		e.capturePendingNamedDamageSourceLKI(&e.pendingTriggers[i].Ctx, ev.Obj, kw, controller)
	}
}

func damageSourceLKIOf(kw damageKeywordLKI, controller state.PlayerID) effects.DamageSourceLKI {
	return effects.DamageSourceLKI{Lifelink: kw.lifelink, Infect: kw.infect,
		Wither: kw.wither, Deathtouch: kw.deathtouch, Controller: controller}
}

func (e *Engine) captureNamedDamageSourceLKI(stack, source state.ObjID, kw damageKeywordLKI, controller state.PlayerID) {
	if e.damageSourceLKI == nil {
		e.damageSourceLKI = make(map[state.ObjID]map[state.ObjID]effects.DamageSourceLKI)
	}
	if e.damageSourceLKI[stack] == nil {
		e.damageSourceLKI[stack] = make(map[state.ObjID]effects.DamageSourceLKI)
	}
	e.damageSourceLKI[stack][source] = damageSourceLKIOf(kw, controller)
}

func (e *Engine) capturePendingNamedDamageSourceLKI(ctx *effects.Ctx, source state.ObjID, kw damageKeywordLKI, controller state.PlayerID) {
	if ctx.DamageSourceLKI == nil {
		ctx.DamageSourceLKI = make(map[state.ObjID]effects.DamageSourceLKI)
	}
	ctx.DamageSourceLKI[source] = damageSourceLKIOf(kw, controller)
}

func cloneDamageSourceLKI(in map[state.ObjID]effects.DamageSourceLKI) map[state.ObjID]effects.DamageSourceLKI {
	out := make(map[state.ObjID]effects.DamageSourceLKI, len(in))
	for id, lki := range in {
		out[id] = lki
	}
	return out
}
