package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// RegenerationDisallowed implements effects.Host for the CantRegenerate
// restriction (Task ce1): reports whether an Effect-registered restriction
// forbids id from regenerating. Consulted by effects.ReplaceDestruction, so
// Incinerate's "creature can't be regenerated this turn" actually blocks the
// shield-consumption path instead of being a Note. Scanning e.active() keeps
// the expiry discipline identical to every other continuous effect: a
// this-turn restriction is UntilEOT and is dropped at cleanup, a permanent-
// sourced one disappears when its source leaves the battlefield.
func (e *Engine) RegenerationDisallowed(id state.ObjID) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantRegenerate" {
			continue
		}
		if e.restrictionApplies(ce, id) {
			return true
		}
	}
	return false
}

// restrictionBlocksTarget reports whether an Effect-registered CantTarget
// restriction (Vines of Vastwood) prevents the player actor from targeting id
// with a spell or ability. Called from rules/stack.go's askTarget alongside
// the protectedFrom check, so a creature granted "can't be the target of
// spells or abilities your opponents control this turn" is actually withheld
// from the opponent's targeting options.
func (e *Engine) restrictionBlocksTarget(id state.ObjID, actor state.PlayerID) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantTarget" {
			continue
		}
		if !e.restrictionApplies(ce, id) {
			continue
		}
		if !e.restrictionActorMatches(ce, actor) {
			continue
		}
		return true
	}
	return false
}

// restrictionApplies reports whether a registered restriction's ValidCard$/
// ValidTarget$ spec selects the object id. A spec containing IsRemembered is
// resolved through the ordinary object matcher with the effect's remembered
// set bound to the SpecContext -- the general filter implements IsRemembered
// (both bare and compound: Card.IsRemembered+Creature keeps both halves),
// which is the dominant shape for Vines/Incinerate; any other spec falls back
// to the same matcher so a restriction that names a quality (CantTarget with
// ValidCard$ Creature, say) still works.
func (e *Engine) restrictionApplies(ce *ContinuousEffect, id state.ObjID) bool {
	spec := ce.RestrictParam(cards.PKValidCard)
	if spec == "" {
		spec = ce.RestrictParam(cards.PKValidTarget)
	}
	if spec == "" {
		// The ValidCards$ plural spelling: Forge allows both on a restriction
		// body, and one CanAttackDefender grant (Wakestone Gargoyle's
		// `ValidCards$ Creature.YouCtrl+withDefender`) spells it. Corpus
		// census: no Cant* body carries ValidCards$ without ValidCard$, so
		// the fallback is unreachable for every pre-existing restriction.
		spec = ce.RestrictParam(cards.PKValidCards)
	}
	if spec == "" {
		return len(ce.Remembered) > 0
	}
	sc := e.specCtx(ce.Source, ce.Controller)
	for _, r := range ce.Remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	return e.matchesSpec(spec, id, sc)
}

// restrictionActorMatches scopes a CantTarget restriction by Activator$:
// Vines of Vastwood's Activator$ Player.Opponent means the restriction only
// bites when the player targeting the creature is an opponent of the effect's
// controller (the caster of Vines). A restriction with no Activator$ applies
// to any actor.
func (e *Engine) restrictionActorMatches(ce *ContinuousEffect, actor state.PlayerID) bool {
	spec, ok := ce.RestrictParamOk(cards.PKActivator)
	if !ok {
		return true
	}
	return effects.MatchesPlayerSpecCtx(e.G, spec, actor, ce.Controller, e.playerSpecCtx(ce.Source))
}

// SacrificeBlocked implements effects.Host for the CantSacrifice restriction
// (task combatrestriction1): reports whether id is forbidden from being
// sacrificed at all — an Effect-registered CantSacrifice restriction (Call for
// Aid's "You can't sacrifice those creatures this turn") or a face
// CantSacrifice static (the simple Card.Self carriers). Consulted at every
// sacrifice candidate choke point (effSacrifice's eligible pool and its
// object-target paths, effSacrificeAll, and the cast/activation/mana/ward/
// unless Sac-cost candidate walks), so a blocked permanent is never offered,
// never asked, and never taken.
//
// forCost (vc-static1) names the call site's provenance: the cost-driven
// callers (the cast/activation/mana/ward/unless Sac-cost candidate walks)
// pass true, the effect-driven ones (effSacrifice, effSacrificeAll) false. A
// face static's ForCost$/ValidCause$ scoping reads the split: ForCost$ False
// lines never restrict a cost sacrifice, ForCost$ True lines restrict only a
// cost sacrifice, and ValidCause$ is evaluated against the cause appropriate
// to the path -- actionCause() (the resolving wrapper) on the effect path,
// the pending cast/activation on the cost path (causeCostAdmits, task
// cantsac1).
func (e *Engine) SacrificeBlocked(id state.ObjID, forCost bool) bool {
	return e.sacrificeBlocked(id, forCost, costCauseNone)
}

// sacrificeBlockedForCost is the cost path's entry point (task cantsac1):
// the rules-side Sac-cost walks (cast/activation, mana ability, ward,
// unless, the cumulative-upkeep/echo Sac arm and the Cost$ Mandatory
// trigger-cost window) know what the sacrifice is paying for, so they call
// this with the cost's own cause (the semantics table on costCause) instead
// of the effects.Host method. causeCostAdmits reads it, so a `ForCost$ True
// | ValidCause$ Spell,Activated` static (angel_of_jubilation,
// yasharn_implacable_earth) blocks a cast/activation cost sacrifice it
// should, leaves an effect's sacrifice alone and scopes past a ward, unless
// or upkeep payment, whose demand is a trigger or a resolution election.
func (e *Engine) sacrificeBlockedForCost(id state.ObjID, cause costCause) bool {
	return e.sacrificeBlocked(id, true, cause)
}

// costCause names what a COST-path sacrifice is being paid for, the cost-side
// counterpart of causeSpecAdmits' actionCause(). A cost has no resolving
// object to attribute: an activated ability's costs are paid BEFORE its stack
// object exists (cast.go pushCast's pc.isAbility() early return), so the
// stack top would name whatever unrelated object was already there -- the
// exact misattribution trigmatch.DiscardCauseAdmits guards against. The pending act of
// casting/activating is therefore the only honest cause where one is pending,
// and the defined semantics per cost site (cantsac1 r2) are:
//
//	spell-cast cost component -> costCauseSpell
//	activated-ability cost component, mana abilities included -> costCauseActivated
//	ward cost (CR 702.22: the ward trigger demands the payment) -> costCauseTriggered
//	cumulative-upkeep payment and the Cost$ Mandatory trigger-cost window
//	(CR 702.25a: the upkeep/resolving trigger demands the payment) -> costCauseTriggered
//	unless payment (paid during a resolving ability to elect its outcome --
//	a resolution-election payment, never a cast or activation cost) -> costCauseResolution
//
// costCauseNone is no cost context at all: the effect path (the effects.Host
// method, forCost false) and a caller with nothing pending. causeCostAdmits
// reads Spell, Activated and Triggered; Resolution is inadmissible by every
// readable base, so a ValidCause$ line fails closed at an unless site (the
// permissive direction) instead of blocking a payment the resolving ability
// did not demand as its cast/activation cost. Every corpus ForCost$ True
// carrier is `ValidCause$ Spell,Activated` (angel_of_jubilation,
// yasharn_implacable_earth), so a ward, unless or upkeep payment is correctly
// OUTSIDE its scope: Angel stops sacrificing "to cast spells or activate
// abilities", and none of those three payments is one.
type costCause uint8

const (
	costCauseNone       costCause = iota // no cost context (the effect path)
	costCauseSpell                       // a component of casting a spell
	costCauseActivated                   // a component of activating an ability
	costCauseTriggered                   // a payment a triggered ability demands (ward, upkeep)
	costCauseResolution                  // an unless payment made during a resolving ability
)

// costCauseForAbility is the offer gate's variant (cast.go nonManaCastable):
// castable prices a HYPOTHETICAL cast with no pendingCast, so the caller's
// own ability bit is the provenance.
func costCauseForAbility(ability bool) costCause {
	if ability {
		return costCauseActivated
	}
	return costCauseSpell
}

func (e *Engine) sacrificeBlocked(id state.ObjID, forCost bool, cause costCause) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantSacrifice" {
			continue
		}
		if e.restrictionApplies(ce, id) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantSacrifice") {
		if !effects.CantSacrificeRestrictionParamsReadable(sv.Params) {
			continue
		}
		// cantsac1: the cause-scoping parameters, evaluated before the
		// ValidCard match so an unevaluable shape stays skipped (the
		// permissive direction) instead of blanket-blocking. ForCost$ True
		// restricts only COST sacrifices; ForCost$ False never restricts one.
		switch sv.ParamStr(cards.PKForCost) {
		case "True":
			if !forCost {
				continue
			}
		case "False":
			if forCost {
				continue
			}
		}
		// ValidCause$ names the kind of spell/ability that must be causing
		// the sacrifice. The effect path (forCost false) has a real
		// resolving wrapper, so actionCause() evaluates it (causeSpecAdmits).
		// The cost path has none, so it evaluates the pending cast/activation
		// identity instead (causeCostAdmits) -- a cost sacrifice is caused by
		// the spell being cast or the ability being activated, never by the
		// object already on the stack.
		if spec := sv.ParamStr(cards.PKValidCause); spec != "" {
			if forCost {
				if !causeCostAdmits(spec, cause) {
					continue
				}
			} else if !e.causeSpecAdmits(spec, sv.Source) {
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidCard); spec != "" &&
			e.matchesSpec(spec, id, e.specCtx(sv.Source, sv.Controller)) {
			return true
		}
	}
	return false
}

// ExileBlocked implements effects.Host for the CantExile restriction: reports
// whether id is forbidden from being exiled by the cause currently in flight
// — an Effect-registered CantExile restriction or a face CantExile static
// (The Master, Multiplied: "Triggered abilities you control can't cause you
// to ... exile creature tokens you control"). Consulted at every
// effect-driven exile candidate choke point in effects/zone.go
// (effChangeZone's object path, effChangeZoneAll's sweep and the shared
// ChangeZone settle), so a blocked permanent is never exiled.
//
// forCost names the call site's provenance exactly as SacrificeBlocked's does:
// the effect-driven callers (this package's own effects.Host consumers) pass
// false, so a ForCost$ False line restricts them and a ForCost$ True line does
// not. The rules-side COST walks call exileBlockedForCost, which carries the
// pending cast/activation so a cost-path ValidCause$ can be evaluated.
func (e *Engine) ExileBlocked(id state.ObjID, forCost bool) bool {
	return e.exileBlocked(id, forCost, costCauseNone)
}

// exileBlockedForCost is the cost path's entry point, the CantExile sibling of
// sacrificeBlockedForCost: the rules-side battlefield-Exile cost walk knows
// what the exile is paying for, so it passes the cost's own cause instead of
// the effects.Host method. causeCostAdmits reads it, so a `ForCost$ True |
// ValidCause$ ...` CantExile static would block a matching cost exile while
// leaving an effect's exile alone. The Master's own line is ForCost$ False, so
// it never restricts a cost path (the permissive direction for a cost
// payment, and the only corpus CantExile carrier).
func (e *Engine) exileBlockedForCost(id state.ObjID, cause costCause) bool {
	return e.exileBlocked(id, true, cause)
}

// exileBlocked is the shared CantExile reader. The continuous branch is
// deliberately unconditional: effEffect's registration gate
// (effects.CantRestrictionParamsReadable) refuses to register any
// cause-scoped CantExile body, so a continuous CantExile reaching this walk
// carries only ValidCard$ and the blanket reading is exact. The face-static
// branch evaluates the full body: ForCost$ against the caller's provenance and
// ValidCause$ against the in-flight cause — actionCause() on the effect path
// (the resolving wrapper at the top of the stack), the pending
// cast/activation identity on the cost path (causeCostAdmits), the same
// classifier discipline sacrificeBlocked keeps.
func (e *Engine) exileBlocked(id state.ObjID, forCost bool, cause costCause) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantExile" {
			continue
		}
		if e.restrictionApplies(ce, id) {
			return true
		}
	}
	for _, sv := range e.activeStatics("CantExile") {
		if !effects.CantExileRestrictionParamsReadable(sv.Params) {
			continue
		}
		// The cause-scoping parameters, evaluated before the ValidCard match
		// so an unevaluable shape stays skipped (the permissive direction)
		// instead of blanket-blocking. ForCost$ True restricts only COST
		// exiles; ForCost$ False never restricts one.
		switch sv.ParamStr(cards.PKForCost) {
		case "True":
			if !forCost {
				continue
			}
		case "False":
			if forCost {
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidCause); spec != "" {
			if forCost {
				if !causeCostAdmits(spec, cause) {
					continue
				}
			} else if !e.causeSpecAdmits(spec, sv.Source) {
				continue
			}
		}
		if spec := sv.ParamStr(cards.PKValidCard); spec != "" &&
			e.matchesSpec(spec, id, e.specCtx(sv.Source, sv.Controller)) {
			return true
		}
	}
	return false
}

// PutCounterBlocked reports whether a counter of kind would be placed on obj
// (object form) or player (player form) is forbidden -- a real CantPutCounter
// restriction static (task cantputcounter1): an Effect-registered one (Melira,
// the Living Cure's "you can't get additional poison counters this turn",
// registered by effEffect from the Effect's StaticAbilities$ NoMorePoison) or
// a face S:Mode$ CantPutCounter static (Solemnity, Melira's Keepers,
// Blightbeetle, Darksteel Angel, Tatterkite, Melira Sylvok Outcast, Phila
// Unsealed). Consulted at the counter-placement choke point in
// rules/replacement.go, BEFORE any AddCounter replacement, so a prohibition
// with no accompanying R:Event$ AddCounter line is still enforced and the
// event is swallowed rather than folded.
//
// Reading (both forms fail closed on the other's event kind): CounterType$
// names the kind (absent = all kinds), ValidPlayer$ scopes the player form,
// ValidCard$/ValidObject$ scopes the object form. An unscoped line blocks
// both forms. Both routes are consulted, mirroring SacrificeBlocked /
// combat.AttackBlocked.
func (e *Engine) PutCounterBlocked(kind string, obj state.ObjID, player state.PlayerID, playerForm bool) bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction != "CantPutCounter" {
			continue
		}
		if !counterKindMatches(ce.RestrictParam(cards.PKCounterType), kind) {
			continue
		}
		if playerForm {
			if spec := strings.TrimSpace(ce.RestrictParam(cards.PKValidPlayer)); spec != "" {
				// Source 0: Player.CardOwner resolution is scoped to CantAttack's
				// Target$ walk; a CardOwner qualifier here fails closed, as before.
				if restrictionPlayerSpecMatches(e.G, spec, player, ce.Controller, 0, ce.RememberedPlayers) {
					return true
				}
				continue
			}
			if strings.TrimSpace(ce.RestrictParam(cards.PKValidCard)) != "" || strings.TrimSpace(ce.RestrictParam(cards.PKValidObject)) != "" {
				continue
			}
			return true
		}
		objSpec := ce.RestrictParam(cards.PKValidCard)
		if objSpec == "" {
			objSpec = ce.RestrictParam(cards.PKValidObject)
		}
		if strings.TrimSpace(objSpec) != "" {
			if e.restrictionApplies(ce, obj) {
				return true
			}
			continue
		}
		if strings.TrimSpace(ce.RestrictParam(cards.PKValidPlayer)) != "" {
			continue
		}
		return true
	}
	for _, sv := range e.activeStatics("CantPutCounter") {
		if !effects.CantPutCounterParamsReadable(sv.Params) {
			continue
		}
		if !counterKindMatches(sv.ParamStr(cards.PKCounterType), kind) {
			continue
		}
		if playerForm {
			if spec := strings.TrimSpace(sv.ParamStr(cards.PKValidPlayer)); spec != "" {
				// Source 0, same scope rule as the continuous-effect branch above.
				if restrictionPlayerSpecMatches(e.G, spec, player, sv.Controller, 0, nil) {
					return true
				}
				continue
			}
			if strings.TrimSpace(sv.ParamStr(cards.PKValidCard)) != "" || strings.TrimSpace(sv.ParamStr(cards.PKValidObject)) != "" {
				continue
			}
			return true
		}
		spec := strings.TrimSpace(sv.ParamStr(cards.PKValidCard))
		if spec == "" {
			spec = strings.TrimSpace(sv.ParamStr(cards.PKValidObject))
		}
		if spec != "" {
			if e.matchesSpec(spec, obj, e.specCtx(sv.Source, sv.Controller)) {
				return true
			}
			continue
		}
		if strings.TrimSpace(sv.ParamStr(cards.PKValidPlayer)) != "" {
			continue
		}
		return true
	}
	return false
}

// counterKindMatches implements a CantPutCounter line's CounterType$ gate: an
// absent kind admits every counter kind, a stated kind matches only the event's
// own, and anything else fails closed.
func counterKindMatches(restriction, kind string) bool {
	restriction = strings.TrimSpace(restriction)
	return restriction == "" || restriction == kind
}

// restrictionPlayerSpecMatches is effects.RestrictionPlayerSpecMatches, the
// restriction player-spec grammar (IsRemembered against a registration's
// captured players, CardOwner against the source's owner), under the name
// this file's PutCounterBlocked selectors and replacement_life.go's
// CantGainLife walk call it by.
func restrictionPlayerSpecMatches(g *state.Game, spec string, defender, controller state.PlayerID, source state.ObjID, rememberedPlayers []state.PlayerID) bool {
	return effects.RestrictionPlayerSpecMatches(g, spec, defender, controller, source, rememberedPlayers)
}

// fogActive reports whether an api:Fog continuous effect (Restriction
// "PreventCombatDamage", effects/fog.go) is currently active. Consulted by
// the combat-damage step's damage passes (rules/combat.go): while it holds,
// no combat damage is dealt that turn. The active() list already applies the
// UntilEOT expiry, so a Fog cast on turn N contributes nothing from turn N+1
// on.
func (e *Engine) fogActive() bool {
	for ceI, ceL := 0, e.active(); ceI < len(ceL); ceI++ {
		ce := &ceL[ceI]
		if ce.Restriction == "PreventCombatDamage" {
			return true
		}
	}
	return false
}
