package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// ClassLevelGainedSub is the level-B sub-family of a "When this Class becomes
// level N" trigger (CR 716.2e). Its cause is the Class's own level-up
// activator, so the recipe in compliance/oraclegen/templates is separate from
// every event cause. Exported so the recipe and this classifier share one
// spelling.
const ClassLevelGainedSub = "trigger.class-level-gained"

// classifyEventTrigger names the sub-family of the trigger shapes whose cause
// a p0-only recipe can produce on turn 1 beyond the original v1 table (spec
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.2): a
// creature or aura-bearer dying, scry and surveil, damage to a player or to
// the card, a loyalty-ability activation, a discard, and a lone attacker. It
// reports ok=false for every other trigger, which classifyTrigger then
// classifies as before. The shapes are disjoint from the v1 ones (a self
// source or a self-dies filter never matches), so running it first moves a
// requirement only between sub-families, never in or out of the requirement
// set.
func classifyEventTrigger(t *cards.Trigger) (sub string, ok bool) {
	if sub, ok := classifyZoneChangeTrigger(t); ok {
		return sub, true
	}
	if sub, ok := classifyRoomTrigger(t); ok {
		return sub, true
	}
	if sub, ok := classifyCounterTrigger(t); ok {
		return sub, true
	}
	if sub, ok := classifyTurnFaceUpTrigger(t); ok {
		return sub, true
	}
	switch t.ModeKind() {
	case cards.TriggerChangesZone, cards.TriggerChangesZoneAll:
		// Any non-self Battlefield->Graveyard trigger, whichever side, type
		// or qualifier its filter names: the recipe picks the victim the
		// filter accepts and names its own skip for one it cannot place.
		if strings.EqualFold(t.ParamStr(cards.PKOrigin), "Battlefield") &&
			strings.EqualFold(t.ParamStr(cards.PKDestination), "Graveyard") &&
			!namesSelf(ZoneChangeFilter(t)) {
			return "trigger.dies-other", true
		}
	case cards.TriggerScry, cards.TriggerSurveil:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
			if t.ModeKind() == cards.TriggerScry {
				return "trigger.scry", true
			}
			return "trigger.surveil", true
		}
	case cards.TriggerDamageDone, cards.TriggerDamageAll:
		// An opponent dealt damage. Combat damage from the card itself stays
		// trigger.combat-damage.
		if !filterHasToken(t.ParamStr(cards.PKValidTarget), "Opponent") || namesSelf(t.ParamStr(cards.PKValidSource)) {
			break
		}
		switch {
		case strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "False"):
			return "trigger.noncombat-damage", true
		case t.ModeKind() == cards.TriggerDamageAll && strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True"):
			return "trigger.combat-damage-all", true
		}
	case cards.TriggerDamageDoneOnce:
		// "whenever this creature is dealt damage": any damage, so a
		// noncombat source (Shock at the card) causes it.
		if namesSelf(t.ParamStr(cards.PKValidTarget)) && !strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") {
			return "trigger.noncombat-damage", true
		}
	case cards.TriggerExcessDamageAll:
		// "Whenever one or more creatures your opponents control are dealt
		// excess noncombat damage": the cause is Shock (2 damage) at a
		// 1-toughness creature p1 controls, one point of excess. A combat
		// shape or a you-control filter stays a gap (the engine matcher
		// would fire, but no probe cause is built for it).
		vt := t.ParamStr(cards.PKValidTarget)
		if !strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") &&
			!filterHasToken(vt, "YouCtrl") && !filterHasToken(vt, "Self") {
			return "trigger.excess-damage", true
		}
	case cards.TriggerAbilityCast:
		if strings.EqualFold(t.ParamStr(cards.PKValidActivatingPlayer), "You") &&
			strings.EqualFold(t.ParamStr(cards.PKValidSA), "Activated.Loyalty") {
			return "trigger.loyalty-activated", true
		}
	case cards.TriggerCounterAddedOnce:
		if strings.EqualFold(t.ParamStr(cards.PKCounterType), "LOYALTY") &&
			strings.EqualFold(t.ParamStr(cards.PKValidSource), "You") {
			return "trigger.loyalty-activated", true
		}
	case cards.TriggerLifeLost:
		// The recipe casts Shock at the losing player (the source's
		// controller for You/Player, an opponent for Opponent); the engine's
		// LifeLost matcher still decides per card whether the loss fires it.
		return "trigger.life-lost", true
	case cards.TriggerDiscarded:
		if namesSelf(t.ParamStr(cards.PKValidCard)) || discardByController(t) {
			return "trigger.discarded", true
		}
	case cards.TriggerDiscardedAll:
		if discardByController(t) {
			return "trigger.discarded", true
		}
	case cards.TriggerAttackersDeclaredOneTarget:
		if namesYouCtrl(t.ParamStr(cards.PKValidAttackers)) && strings.EqualFold(t.ParamStr(cards.PKAttackedTarget), "Player") {
			return "trigger.attacks-one-target", true
		}
	case cards.TriggerCaseSolved:
		// "Whenever you solve a Case" (Case File Auditor): the cause is a
		// probe Case solved by its own end-step "To solve" trigger (CR
		// 719.3a), whose Solved grant the engine's matcher reads.
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You") {
			return "trigger.case-solved", true
		}
	case cards.TriggerAbilityTriggered:
		// "Whenever a creature you control attacking causes a triggered
		// ability of that creature to trigger" (Firebender Ascension): the
		// cause is p0 attacking with a probe creature that has its own
		// attack trigger, which emits the engine's AbilityTriggered marker.
		// A ValidDestination$/ValidSpellAbility$ shape stays a gap (the
		// marker does not model them and the matcher fails closed).
		if strings.EqualFold(t.ParamStr(cards.PKTriggeredOwnAbility), "True") &&
			t.ParamStr(cards.PKValidDestination) == "" && t.ParamStr(cards.PKValidSpellAbility) == "" &&
			strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidMode)), "attacks") &&
			strings.Contains(strings.ToLower(t.ParamStr(cards.PKValidSource)), "youctrl") {
			return "trigger.ability-triggered", true
		}
	case cards.TriggerClassLevelGained:
		// "When this Class becomes level N": the card's own Class. The body
		// carries ClassBand$ N (the level that fires it).
		if namesSelf(t.ParamStr(cards.PKValidCard)) {
			return ClassLevelGainedSub, true
		}
	}
	if sub, ok := classifyDamageTrigger(t); ok {
		return sub, true
	}
	if sub, ok := classifyKeywordActionTrigger(t); ok {
		return sub, true
	}
	return classifySacrificeTrigger(t)
}

// discardByController reports whether a discard trigger's ValidPlayer names
// the trigger's own controller ("you", "player" or absent) and its ValidCard
// filter does not restrict the discarded card to one an opponent owns or
// controls. The level-B discard recipe makes the controller discard a probe
// creature, so only these shapes are servable; an opponent-scoped discard
// stays a gap.
func discardByController(t *cards.Trigger) bool {
	switch strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKValidPlayer))) {
	case "", "you", "player":
	default:
		return false
	}
	filter := strings.ToLower(t.ParamStr(cards.PKValidCard))
	return !strings.Contains(filter, "oppown") && !strings.Contains(filter, "oppctrl")
}

// selfCastTrigger reports the narrow self-cast shape that the level-A
// cast-resolve scenario is known to settle: Emrakul's unconditional
// TrigUntapAll trigger. Other unconditional self-cast triggers may require a
// target or choice that the level-A scenario does not supply (for example,
// Ulamog's TrigChange), so they must remain level-B gaps until that scenario
// proves their resolution too.
func selfCastTrigger(f *cards.Face, t *cards.Trigger) bool {
	if f.IsLand() || !strings.EqualFold(t.ParamStr(cards.PKValidCard), "Card.Self") ||
		!strings.EqualFold(t.ParamStr(cards.PKExecute), "TrigUntapAll") {
		return false
	}
	for k := range t.Params {
		switch k {
		case "Mode", "Execute", "TriggerDescription", "ValidCard":
		default:
			return false
		}
	}
	return true
}
