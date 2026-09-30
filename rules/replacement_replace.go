package rules

import (
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
	"strconv"
	"strings"
)

// ReplaceEvent implements effects.Host. It rewrites the amount of the Damage
// event currently being replaced -- the one held in e.replacingEvent -- from
// either a resolved numeric value (a literal, or a Count$/SVar expression
// effects.Num already evaluated in the replacement source's context) or a
// ReplaceCount$ body, whose base is the HELD event's own amount and which
// only the host reading the in-flight event can resolve. Anything else -- an
// unresolvable value, an unknown field, a non-Damage event -- leaves the held
// event untouched: an amount replacement that cannot be computed is closer to
// the card than one that erases the damage or discards the event.
func (e *Engine) ReplaceEvent(name, raw string, resolved int32) {
	ev := e.replacingEvent
	if ev == nil {
		return
	}
	if ev.Kind == events.PlanarRoll || ev.Kind == events.RollDice {
		// The roll rewrite (task rollplanar1's Ichor Elixir pair, extended to
		// the regular-dice proposal in rolldice-repl -- Wyll, Blade of
		// Frontiers; Barbarian Class; Pixie Guide): Number is the dice count
		// (the held event's Amount), Ignore the ignored-result count (the held
		// event's Counter, decimal; "" reads 0). A resolved literal value
		// applies as-is; an unresolvable ReplaceCount body leaves the field
		// alone, the same fail-closed direction the Damage arm keeps. Any
		// other VarName (SwapRoll's DicePTExchanges) is unmodelled and leaves
		// the proposal untouched; the dispatch reports that inertness loudly.
		// The planar-dice rewrite (Ichor Elixir's ReplaceEffect pair): Number
		// is the dice count (the held event's Amount), Ignore the ignored-
		// result count (the held event's Counter, decimal; "" reads 0). A
		// resolved literal value applies as-is; an unresolvable ReplaceCount
		// body leaves the field alone, the same fail-closed direction the
		// Damage arm keeps.
		if body, ok := strings.CutPrefix(raw, "ReplaceCount$"); ok {
			field, op, hasOp := strings.Cut(body, "/")
			if !hasOp {
				return
			}
			switch field {
			case "Number":
				ev.Amount = replCountOp(ev.Amount, op)
			case "Ignore":
				base := int32(0)
				if n, err := strconv.Atoi(ev.Counter); err == nil {
					base = int32(n)
				}
				ev.Counter = strconv.FormatInt(int64(replCountOp(base, op)), 10)
			}
			return
		}
		if name == "Number" && resolved > 0 {
			ev.Amount = resolved
		} else if name == "Ignore" && resolved > 0 {
			ev.Counter = strconv.FormatInt(int64(resolved), 10)
		}
		return
	}
	if ev.Kind != events.Damage {
		return
	}
	if body, ok := strings.CutPrefix(raw, "ReplaceCount$"); ok {
		field, op, hasOp := strings.Cut(body, "/")
		if (field != "DamageAmount" && field != "Amount") || !hasOp {
			return
		}
		ev.Amount = replCountOp(ev.Amount, op)
		return
	}
	if (name == "DamageAmount" || name == "Amount") && resolved > 0 {
		ev.Amount = resolved
		return
	}
	if name != "Affected" {
		return
	}
	switch raw {
	case "You":
		ev.Obj, ev.Player = 0, e.controllerOf(e.replacingSource)
	case "Self":
		ev.Obj, ev.Player = e.replacingSource, 0
	case "Enchanted", "Equipped":
		if source := e.G.Obj(e.replacingSource); source != nil && source.AttachedTo != 0 {
			ev.Obj, ev.Player = source.AttachedTo, 0
		}
	case "ReplacedSourceController":
		if source := e.G.Obj(e.damaging); source != nil {
			ev.Obj, ev.Player = 0, source.Controller
		}
	case "ReplacedTargetController":
		if target := e.G.Obj(ev.Obj); target != nil {
			ev.Obj, ev.Player = 0, target.Controller
		}
	case "Remembered":
		// Infer the destination kind from the captured GameEntity, rather than
		// parsing VarType$. Remembered permanents must still be on the
		// battlefield; remembered players are valid damage recipients while
		// they remain in the game. With no live referent the held event is
		// unchanged (fail closed).
		for _, target := range e.replRemembered {
			if target.IsPlayer {
				if int(target.Player) < len(e.G.Players) && !e.G.Players[target.Player].Lost {
					ev.Obj, ev.Player = 0, target.Player
					break
				}
				continue
			}
			if obj := e.G.Obj(target.Obj); target.Obj != 0 && obj != nil && obj.Zone == state.ZBattlefield {
				ev.Obj, ev.Player = target.Obj, 0
				break
			}
		}
	}
	// CR 702.90b: the infect marker on a Damage event encodes the FORM the
	// damage is dealt in, and the form depends on the RECIPIENT. A redirect
	// just changed the recipient (a player-targeted hit moved onto a
	// permanent, or vice versa), so the marker's recipient half is recomputed
	// here. The source-infect fact is preserved: the marker is only ever set
	// by an emitter whose source had infect, so a non-empty marker still means
	// infect. Without this a bare "infect" (player form) survives onto a
	// creature recipient: events.Apply treats a bare marker on an object as
	// ordinary marked damage while convertInfectDamage then also emits -1/-1
	// counters, so a redirected infect hit would land in BOTH forms.
	e.recomputeInfectMarker(ev)
	e.recomputeWitherMarker(ev)
}

// replCountOp applies Forge's ReplaceCount$ arithmetic to a base amount: the
// corpus carries Twice (Bloodletter of Aclazotz), Thrice (Fiery Emancipation)
// and Plus.N (Torture Pit). The arithmetic is the ONE shared implementation,
// effects.ApplyCountOp -- so the next op added to the /Op vocabulary (this
// adapter previously duplicated the switch by hand and silently lacked
// Negative and the whole Divide family) reaches ReplaceCount$ for free and
// the count grammar and the replacement grammar cannot drift.
//
// The one thing ReplaceCount$ adds over the count grammar is the CLAMP: a
// replacement cannot deal, gain or place a negative amount, so a body whose
// arithmetic drives the base below zero yields 0 (the count grammar has no
// such clamp -- a negative count is meaningful there). An op the shared
// applier does not parse is returned unchanged by it, the same fail-closed
// direction this adapter always kept.
func replCountOp(base int32, op string) int32 {
	v := effects.ApplyCountOp(base, op)
	if v < 0 {
		return 0
	}
	return v
}

// replacementCondition reads the common CheckSVar$/SVarCompare$ gate (Phial
// of Galadriel) from the replacement source's current context.
func (e *Engine) replacementCondition(source state.ObjID, r *cards.Repl) bool {
	if !e.classBandGateHolds(r.Params, source) {
		return false
	}
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return false
	}
	if spec := r.Params["IsPresent"]; spec != "" {
		found := false
		for _, p := range e.G.AliveFrom(0) {
			for _, id := range e.G.Zone(state.ZBattlefield, p) {
				if e.matchesSpec(spec, id, e.specCtx(source, o.Controller)) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	name := r.Params["CheckSVar"]
	if name == "" {
		return true
	}
	value := effects.EvalCount(e, &effects.Ctx{Source: source, Controller: o.Controller, SVars: o.Face().SVars}, o.Face().SVars[name])
	return compareLife(value, r.Params["SVarCompare"])
}

// replaceCount evaluates ReplaceEffect's ReplaceCount$Amount/LifeGained
// forms. It follows SVar indirection and supports the three corpus operators:
// Twice, Plus.N and LimitMax.<SVar>.
func (e *Engine) replaceCount(source state.ObjID, r *cards.Repl, name string, amount int32) (int32, bool) {
	op, ok := e.replaceCountOp(source, r, name)
	if !ok {
		return 0, false
	}
	o := e.G.Obj(source)
	if op == "/Twice" {
		return amount * 2, true
	}
	if n, err := strconv.ParseInt(strings.TrimPrefix(op, "/Plus."), 10, 32); strings.HasPrefix(op, "/Plus.") && err == nil {
		return amount + int32(n), true
	}
	if arg, ok := strings.CutPrefix(op, "/LimitMax."); ok {
		limit := effects.EvalCount(e, &effects.Ctx{Source: source, Controller: o.Controller, SVars: o.Face().SVars}, o.Face().SVars[arg])
		if limit < 0 {
			limit = 0
		}
		if amount > limit {
			amount = limit
		}
		return amount, true
	}
	return 0, false
}

// replaceCountOp returns the operator suffix ("/Twice", "/Plus.1", ...) of a
// ReplaceEffect body's ReplaceCount$<name> value, following SVar indirection.
func (e *Engine) replaceCountOp(source state.ObjID, r *cards.Repl, name string) (string, bool) {
	if r.With == nil || r.With.API != "ReplaceEffect" || !strings.EqualFold(r.With.Params["VarName"], name) {
		return "", false
	}
	expr := r.With.Params["VarValue"]
	o := e.G.Obj(source)
	if o == nil || o.Face() == nil {
		return "", false
	}
	if body, ok := o.Face().SVars[expr]; ok {
		expr = body
	}
	prefix := "ReplaceCount$" + name
	if !strings.HasPrefix(expr, prefix) {
		return "", false
	}
	return strings.TrimPrefix(expr, prefix), true
}

func replacementActive(e *Engine, source state.ObjID, r *cards.Repl) bool {
	if !e.commandReplZoneAdmits(*r, source) {
		return false
	}
	active, ok := r.Params["ActiveZones"]
	if !ok {
		return true
	}
	o := e.G.Obj(source)
	return o != nil && zoneSpecContains(active, o.Zone)
}

func replacementPlayerMatches(e *Engine, source state.ObjID, r *cards.Repl, p state.PlayerID) bool {
	if int(p) >= len(e.G.Players) {
		return false
	}
	v := r.Params["ValidPlayer"]
	return v == "" || effects.MatchesPlayerSpec(e.G, v, p, e.controllerOf(source))
}
