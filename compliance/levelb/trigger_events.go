package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

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
	switch t.ModeKind() {
	case cards.TriggerChangesZone:
		if strings.EqualFold(t.ParamStr(cards.PKOrigin), "Battlefield") &&
			strings.EqualFold(t.ParamStr(cards.PKDestination), "Graveyard") &&
			!namesSelf(t.ParamStr(cards.PKValidCard)) && diesOther(t.ParamStr(cards.PKValidCard)) {
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
	case cards.TriggerDiscarded:
		if namesSelf(t.ParamStr(cards.PKValidCard)) {
			return "trigger.discarded", true
		}
	case cards.TriggerDiscardedAll:
		if strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "Player") {
			return "trigger.discarded", true
		}
	case cards.TriggerAttackersDeclaredOneTarget:
		if namesYouCtrl(t.ParamStr(cards.PKValidAttackers)) && strings.EqualFold(t.ParamStr(cards.PKAttackedTarget), "Player") {
			return "trigger.attacks-one-target", true
		}
	}
	return "", false
}

// diesOther reports whether a ValidCard filter names a creature the card's
// controller controls (`Creature.YouCtrl`, `Creature.Other+YouCtrl`) or the
// creature the card is attached to (`Card.AttachedBy`): in either case a
// Grizzly Bears on p0's battlefield that dies is the cause.
func diesOther(filter string) bool {
	for _, alt := range strings.Split(filter, ",") {
		if filterHasToken(alt, "Creature") && filterHasToken(alt, "YouCtrl") {
			return true
		}
		if filterHasToken(alt, "AttachedBy") {
			return true
		}
	}
	return false
}

// selfCastTrigger reports whether t is an unconditional "when you cast this
// spell" trigger on a spell face: ValidCard `Card.Self` and nothing but the
// mode, the executed ability and the description (no condition, no optional
// decider, no zone filter). The level-A cast-resolve scenario casts the card
// from hand, so the trigger goes on the stack above the spell and the
// scenario's resolve step resolves it; Emrakul, the Exigent Doom's committed
// level-A verdict freezes exactly that (stack [ability, spell] after the cast,
// empty after the resolve). A conditional or optional cast trigger may not
// fire in that scenario, so it stays a gap.
func selfCastTrigger(f *cards.Face, t *cards.Trigger) bool {
	if f.IsLand() || !strings.EqualFold(t.ParamStr(cards.PKValidCard), "Card.Self") {
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
