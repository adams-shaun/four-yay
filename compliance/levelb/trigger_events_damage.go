package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// DamageSub is the level-B sub-family of the damage triggers a single
// attacking creature or a Shock probe can cause: a source creature's combat
// damage to a player, and noncombat damage to the card itself, the creature it
// is attached to, another creature, or a player. Its causes are the attack and
// Shock steps in compliance/oraclegen/templates, separate from the narrower
// noncombat-damage / combat-damage / combat-damage-all families. Exported so
// the recipe and this classifier share one spelling.
const DamageSub = "trigger.damage"

// classifyDamageTrigger names the damage-trigger shapes the attack/Shock
// recipe can cause (Forge Mode$ DamageDone, DamageDealtOnce, DamageDoneOnce
// and DamageAll). It runs after the narrower noncombat-damage, combat-damage
// and combat-damage-all cases, so those keep their sub-families; the recipe
// reports a named skip for a per-card shape it cannot place (no matching
// source creature, a face-down source, a ValidCause$ the Shock probe does not
// meet). It returns ok=false for every other trigger.
func classifyDamageTrigger(t *cards.Trigger) (sub string, ok bool) {
	switch t.ModeKind() {
	case cards.TriggerDamageDone, cards.TriggerDamageDealtOnce,
		cards.TriggerDamageDoneOnce, cards.TriggerDamageAll:
	default:
		return "", false
	}
	// A DamageDone trigger whose source is the card itself and that demands
	// combat damage is trigger.combat-damage's shape, served by its own
	// recipe.
	if t.ModeKind() == cards.TriggerDamageDone &&
		namesSelf(t.ParamStr(cards.PKValidSource)) &&
		strings.EqualFold(t.ParamStr(cards.PKCombatDamage), "True") {
		return "", false
	}
	return DamageSub, true
}
