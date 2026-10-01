package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Static control statics: an S:Mode$ Continuous static carrying GainControl$
// (Mind Control's "You control enchanted creature", Fealty to the Realm's
// "The monarch controls enchanted creature") is realized here, on top of the
// ordinary control-grant tracker (rules/control.go) rather than as a layer-2
// view. The scan (rules/layers.go's staticEffects) stays PURE: it emits a
// ContinuousEffect carrying only the GainControl$ value and the Affected$
// spec, no callbacks, no emits. The reconcile below is the one place the
// static becomes real:
//
//   - reconcileControlStatics runs at the same fixed points expireControl
//     does (Engine.emit's tail for every state-changing event, and the SBA
//     safety net), derives the WANTED set -- for every live GainControl
//     static, every battlefield object its Affected$ matches, the player the
//     value names -- and registers a real tracked grant plus emits
//     events.ControlChange wherever the object's controller actually differs;
//
//   - ENDING is not the reconcile's job: a static grant is marked `static`
//     and its grantEnded reads the same wanted set -- a grant whose static
//     stopped being live (source left the battlefield, the "as long as" gate
//     flipped, the Aura moved bearers, the named player changed) ends inside
//     the ordinary expireControl pass, and the bearer returns to its
//     Previous controller through the ordinary chain. That is what keeps the
//     stacking contract: a Threaten (temporary steal) on top of a Mind
//     Control static stacks in begin order, and when the Threaten expires
//     the bearer returns to the static's controller, not its owner.
//
// Everything is a deterministic function of the current state driven from
// the same emit paths a replay re-executes, so a replayed game re-derives
// identical grants and identical ControlChange events. No map is ranged
// where order can reach an event: seats walk AliveFrom(0), zones are slices,
// the static memo is the scan's own emission order.

// staticWant is one wanted static control transfer: the SOURCE object (the
// Aura carrying the static, stamped so a returned Aura is a new object, CR
// 400.7), the bearer it currently holds, and the controller the static names.
type staticWant struct {
	source      state.ObjID
	obj         state.ObjID
	sourceStamp uint32
	objStamp    uint32
	controller  state.PlayerID
}

// staticControlWants derives the wanted set fresh from the live static scan.
// The static memo (Engine.staticContinuous, layers.go) is refreshed first
// under the same epoch discipline active() uses, so the two can never read
// different boards. With no GainControl static anywhere the derivation is
// the memo epoch guard plus one slice walk -- the common game's cost.
func (e *Engine) staticControlWants() []staticWant {
	e.refreshStaticContinuous()
	var w []staticWant
	for ceI, ceL := 0, e.staticContinuous; ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.GainControl == "" {
			continue
		}
		ctrl, ok := staticGainControlController(e.G, ce.GainControl, ce.Controller, ce.Source)
		if !ok {
			// A value that names nobody (or several players) grants
			// nothing -- the fail-closed direction, no Note (the scan's
			// consumers cannot emit).
			continue
		}
		src := e.G.Obj(ce.Source)
		if src == nil || src.Zone != state.ZBattlefield {
			continue
		}
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				if !e.matchesSpecFrom(ce.Affects, id, ce.Controller, ce.Source) {
					continue
				}
				o := e.G.Obj(id)
				if o == nil || o.Zone != state.ZBattlefield {
					continue
				}
				w = append(w, staticWant{source: ce.Source, obj: id,
					sourceStamp: src.Timestamp, objStamp: o.Timestamp, controller: ctrl})
			}
		}
	}
	return w
}

// poolHasControlStatic reports whether any card this match can put into play
// may carry a GainControl$ static (cards.MayCarryControlStatic over the decks
// and the token table), the genesis-time gate of reconcileControlStatics.
func poolHasControlStatic(cfg Config) bool {
	for _, deck := range cfg.Decks {
		for _, c := range deck {
			if c.MayCarryControlStatic() {
				return true
			}
		}
	}
	for _, c := range cfg.Tokens {
		if c.MayCarryControlStatic() {
			return true
		}
	}
	return false
}

// staticGainControlController resolves a GainControl$ static VALUE to the
// player who takes the affected objects. "You" is the static's controller
// (ce.Controller -- the Aura's controller, live every re-derivation). Any
// other value is a player spec resolved through the shared grammar
// (MatchesPlayerSpecFrom): it must be QUALIFIED (contain a dot -- a bare
// Player/Any matches every seat and is never a meaningful controller) and
// must match EXACTLY ONE seat; zero or several matches fail closed, the
// ParseControlDuration convention (an unresolvable lifetime never steals).
func staticGainControlController(g *state.Game, spec string, you state.PlayerID, source state.ObjID) (state.PlayerID, bool) {
	if strings.EqualFold(strings.TrimSpace(spec), "You") {
		return you, true
	}
	if !strings.Contains(spec, ".") {
		return 0, false
	}
	found, n := state.PlayerID(0), 0
	for _, p := range g.AliveFrom(0) {
		if effects.MatchesPlayerSpecFrom(g, spec, p, you, source) {
			found, n = p, n+1
			if n > 1 {
				return 0, false
			}
		}
	}
	if n == 1 {
		return found, true
	}
	return 0, false
}

// staticGrantLive reports whether the static-derived grant g's static is
// still live with the SAME resolved controller. Called from grantEnded
// inside expireControl; the derivation is a pure function of current state,
// so a mid-pass re-derivation (the ControlChange emits advance the log
// epoch) is still correct.
func (e *Engine) staticGrantLive(g controlGrant) bool {
	for _, w := range e.staticControlWants() {
		if w.source == g.Source && w.sourceStamp == g.SourceStamp &&
			w.obj == g.Obj && w.objStamp == g.ObjStamp {
			return w.controller == g.Controller
		}
	}
	return false
}

// staticGrantTracked reports whether the wanted transfer w already has its
// tracked grant (same source/bearer stamps and controller) in the tracker.
func (e *Engine) staticGrantTracked(w staticWant) bool {
	for _, g := range e.controlGrants {
		if g.static && g.Source == w.source && g.SourceStamp == w.sourceStamp &&
			g.Obj == w.obj && g.ObjStamp == w.objStamp && g.Controller == w.controller {
			return true
		}
	}
	return false
}

// reconcileControlStatics registers a tracked grant (and emits the matching
// events.ControlChange) for every wanted transfer not yet tracked. It runs
// AFTER expireControl at every shared call site, so a static that stopped
// being live has already ended (its grantEnded read the fresh wanted set)
// and the reconcile only ever ADDS. Idempotent per (source, sourceStamp,
// obj, objStamp, controller) -- the memo re-runs once per event, so the
// tracked check is what keeps a standing grant from re-registering. The
// re-entry guards mirror expireControl's: the ControlChange emits here
// re-enter the emit tail (whose expireControl and reconcile both early-
// return on the flags) and the deferred clears restore the ordinary path.
func (e *Engine) reconcileControlStatics() {
	if e.expiringControl || e.reconcilingControlStatics || !e.controlStaticInPool {
		// No card this match can put into play carries a GainControl$
		// static (poolHasControlStatic), so the wanted set is empty on every
		// board and the scan -- which refreshes the whole static memo first
		// -- would register nothing.
		return
	}
	e.reconcilingControlStatics = true
	defer func() { e.reconcilingControlStatics = false }()
	for _, w := range e.staticControlWants() {
		if e.staticGrantTracked(w) {
			continue
		}
		o, src := e.G.Obj(w.obj), e.G.Obj(w.source)
		if o == nil || o.Zone != state.ZBattlefield || o.Timestamp != w.objStamp ||
			src == nil || src.Zone != state.ZBattlefield || src.Timestamp != w.sourceStamp {
			continue
		}
		if o.Controller == w.controller {
			continue
		}
		prev := o.Controller
		gr := effects.ControlGrant{
			Obj: w.obj, ObjStamp: w.objStamp,
			Previous: prev, Controller: w.controller,
			You: w.controller, Source: w.source, SourceStamp: w.sourceStamp,
		}
		// CR 611.2b: an effect whose "for as long as" duration has already
		// ended when it would begin does nothing -- the same gate the API
		// path (effGainControl) runs before its own emit. Without it a
		// controller who has left the game would be re-registered every
		// emit and re-ended by grantEnded's CR 800.4a check, a grant
		// ping-pong no board state justifies.
		if effects.ControlGrantEnded(e, gr) {
			continue
		}
		// The same registration pattern the API path uses: emit the
		// ControlChange FIRST, then record the grant whose Previous is the
		// controller immediately before the effect.
		e.emit(events.Event{Kind: events.ControlChange, Obj: w.obj, Player: w.controller})
		before := len(e.controlGrants)
		e.RegisterControl(gr)
		if len(e.controlGrants) > before {
			e.controlGrants[len(e.controlGrants)-1].static = true
		}
	}
}
