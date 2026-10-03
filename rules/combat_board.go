package rules

import (
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// combat_board.go is package rules' side of the rules/combat seam (spec
// docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md §9, W5
// step E5): combatBoard implements combat.Board over the Engine, so the
// attack/block legality predicates read the game without holding the
// engine. Every method is a pure read that forwards to the engine reader the
// predicate called before it moved, so the predicates' answers -- and every
// decision, event and chain head built from them -- are unchanged.
//
// combatBoard is the Engine itself under another method set: asBoard is a
// pointer conversion, so passing it as a combat.Board neither allocates nor
// copies, and its methods are not Engine methods.
type combatBoard Engine

var _ combat.Board = (*combatBoard)(nil)

// asBoard views e as the combat package's read-only Board.
func asBoard(e *Engine) *combatBoard { return (*combatBoard)(e) }

// combatKeywordHeads maps the combat package's keyword names to the
// precompiled heads rules' keyword reads use.
var combatKeywordHeads = [combat.NumKeywords]kwHead{
	combat.KWDefender:     kwhDefender,
	combat.KWHaste:        kwhHaste,
	combat.KWUnleash:      kwhUnleash,
	combat.KWShadow:       kwhShadow,
	combat.KWFear:         kwhFear,
	combat.KWIntimidate:   kwhIntimidate,
	combat.KWHorsemanship: kwhHorsemanship,
	combat.KWFlying:       kwhFlying,
	combat.KWReach:        kwhReach,
	combat.KWSkulk:        kwhSkulk,
}

func (b *combatBoard) Game() *state.Game { return b.G }

func (b *combatBoard) Log() []events.Event { return b.L.Events }

func (b *combatBoard) IsCreature(id state.ObjID) bool {
	e := (*Engine)(b)
	return e.IsCreature(id)
}

func (b *combatBoard) HasKW(id state.ObjID, kw combat.Keyword) bool {
	e := (*Engine)(b)
	return e.hasKeywordH(id, combatKeywordHeads[kw])
}

func (b *combatBoard) Keywords(id state.ObjID) []string {
	e := (*Engine)(b)
	return e.Derived(id).Keywords
}

func (b *combatBoard) Power(id state.ObjID) int32 {
	e := (*Engine)(b)
	return e.Derived(id).Power
}

func (b *combatBoard) ObjectColors(o *state.Object) string {
	e := (*Engine)(b)
	return e.objColors(o)
}

func (b *combatBoard) ProtectedFrom(target, source state.ObjID) bool {
	e := (*Engine)(b)
	return e.protectedFrom(target, source)
}

func (b *combatBoard) Active() []state.ContinuousEffect {
	e := (*Engine)(b)
	return e.active()
}

func (b *combatBoard) RestrictionApplies(ce *state.ContinuousEffect, id state.ObjID) bool {
	e := (*Engine)(b)
	return e.restrictionApplies(ce, id)
}

// combatStaticsBuf is one mode's combat.Static view of activeStatics(mode),
// reused across calls (engineScratch.combatStatics). A nested call for the
// same mode rewrites the buffer with the same lines -- the board cannot
// change under a pure read -- so an outer range over it reads unchanged
// values.
type combatStaticsBuf struct {
	mode string
	buf  []combat.Static
}

// Statics is activeStatics(mode) as combat.Static values. Every view
// activeStatics returns is a printed battlefield static carrying exactly
// Source, Controller, Params, PS and SVars (scanActiveStatics and the fused
// walk-cache scan build no other field), so the conversion is lossless and
// staticViewOf rebuilds the identical staticView for the gate and spec reads.
func (b *combatBoard) Statics(mode string) []combat.Static {
	e := (*Engine)(b)
	svs := e.activeStatics(mode)
	if len(svs) == 0 {
		return nil
	}
	var slot *combatStaticsBuf
	for i := range e.combatStatics {
		if e.combatStatics[i].mode == mode {
			slot = &e.combatStatics[i]
			break
		}
	}
	if slot == nil {
		e.combatStatics = append(e.combatStatics, combatStaticsBuf{mode: mode})
		slot = &e.combatStatics[len(e.combatStatics)-1]
	}
	out := slot.buf[:0]
	for i := range svs {
		sv := &svs[i]
		out = append(out, combat.Static{Source: sv.Source, Controller: sv.Controller,
			Params: sv.Params, PS: sv.PS, SVars: sv.SVars})
	}
	slot.buf = out
	return out
}

// staticViewOf is the staticView a combat.Static was converted from.
func staticViewOf(sv combat.Static) staticView {
	return staticView{Source: sv.Source, Controller: sv.Controller, Params: sv.Params, PS: sv.PS, SVars: sv.SVars}
}

func (b *combatBoard) StaticGateHolds(sv combat.Static) bool {
	e := (*Engine)(b)
	return e.continuousGateHolds(staticViewOf(sv))
}

// MatchesSpec builds the (source, you) spec context engine-side -- specCtx
// carries the numeric-RHS Resolve closure, which stays on this frame's stack
// only while the context never crosses an interface -- and binds the
// registration's remembered objects and captured players exactly as the
// predicate did before it moved.
func (b *combatBoard) MatchesSpec(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	e := (*Engine)(b)
	sc := e.specCtx(source, you)
	for _, r := range remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	sc.RememberedPlayers = rememberedPlayers
	return e.matchesSpec(spec, id, sc)
}

func (b *combatBoard) MatchesStaticSpec(spec string, id state.ObjID, sv combat.Static) bool {
	e := (*Engine)(b)
	return e.matchesSpec(spec, id, e.staticSpecCtx(staticViewOf(sv)))
}

// GoadMatches brackets the match in the goadProbe re-entry guard (see the
// engine field): a goad line's IsGoaded-conditioned Affected$ spec must not
// re-derive the static-goad set it is part of.
func (b *combatBoard) GoadMatches(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID) bool {
	e := (*Engine)(b)
	sc := e.specCtx(source, you)
	for _, r := range remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	e.goadProbe++
	defer func() { e.goadProbe-- }()
	return e.matchesSpec(spec, id, sc)
}

func (b *combatBoard) PlayerSpecCtx(source state.ObjID) effects.PlayerSpecCtx {
	e := (*Engine)(b)
	return e.playerSpecCtx(source)
}

// LandSpecCtx is the landwalk land filter's context. It reads the attacker's
// characteristics FIRST: Derived runs active()'s static scan, which is what
// ARMS layer4InPool when the only type-changing carrier is a permanent placed
// outside the genesis deck pool (a fixture's direct AddObject, a token or a
// copy). Only then can the refresh below be gated correctly; checking the
// flag before this read would skip the refresh on exactly that board and
// read a stale derived-type table. The call is allocation-free (layers_test.go
// pins AllocsPerRun == 0), so the predicate's later keyword read is a cheap
// cached re-read, not a copy. The refresh keeps the layer-4 table in step
// with the board (a direct AddObject placement in a fixture emits nothing, so
// the epoch guard inside would otherwise stay a stale cache hit), gated so a
// match with no type-changing carrier pays one branch. withNames is the ONE
// seam for a hand-built rules SpecContext (setname.go): it binds the layer-3
// rename set and the layer-4 derived type table, so the land filter ages with
// the layer walk instead of hand-copying one table.
func (b *combatBoard) LandSpecCtx(defender state.PlayerID, attacker state.ObjID) effects.SpecContext {
	e := (*Engine)(b)
	_ = e.Derived(attacker)
	if e.layer4InPool {
		e.refreshDerivedTypes()
	}
	return e.withNames(effects.NewSpecContext(defender, attacker))
}

// staticallyGoaded is combat.StaticallyGoaded for the readers that publish
// the static-goad set (host_read.go's layer tables, statics.go's matchesSpec
// bind, the trigger matcher).
func (e *Engine) staticallyGoaded() map[state.ObjID]bool {
	return combat.StaticallyGoaded(asBoard(e), nil)
}

// staticallyGoadedWithLKI also evaluates a just-departed battlefield object's
// LKI against the live goad statics (see combat.StaticallyGoaded).
func (e *Engine) staticallyGoadedWithLKI(lki *state.Object) map[state.ObjID]bool {
	return combat.StaticallyGoaded(asBoard(e), lki)
}

// MustAttackParamsReadableForRules is the face S:-line half of
// effects.MustAttackParamsReadable, and DELEGATES to
// effects.MustAttackParamsReadableForRules so the face and Effect routes can
// never diverge on what is enforceable: there is one whitelist home, not a
// copy kept in step by hand. The face list is the Effect registration list
// EXTENDED by exactly the condition-gate keys -- the gate evaluator,
// continuousGateHolds, is rules-side, so the face route can evaluate those
// gates while the Effect-delivered registration path cannot.
func MustAttackParamsReadableForRules(params map[string]string) bool {
	return effects.MustAttackParamsReadableForRules(params)
}

// CantAttackParamsReadableForRules is the face S:-line half of
// effects.CantRestrictionParamsReadable, and DELEGATES to
// effects.CantAttackParamsReadableForRules so the face reader and the
// whitelist can never diverge on what is enforceable. The face list is the
// CantRestrictionParamsReadable core EXTENDED by exactly the conditional
// parameter family combat.AttackBlocked reads -- UnlessDefender$ (through
// effects.UnlessDefenderHolds) and CheckSVar$/SVarCompare$/Condition$
// (through continuousGateHolds): the face route can evaluate those, the
// Effect-delivered registration path cannot, so a gate-bearing line is
// face-readable while the Effect whitelist stays at the core set (a
// gate-bearing Effect body must not register blanket). The delegation is one
// whitelist home, not a copy kept in step by hand.
func CantAttackParamsReadableForRules(params map[string]string) bool {
	return effects.CantAttackParamsReadableForRules(params)
}
