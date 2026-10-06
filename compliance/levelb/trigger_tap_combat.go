package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// classifyTapCombatTrigger admits the tap and combat-declaration shapes whose
// cause the level-B recipes can build from a combat the generator already
// plays: an attack (which taps the attacker), a block, an attachment, or a
// tap spell. Gorge's own trigger matcher still decides, per card, whether the
// cause fires. It reports ok=false for Static$ True triggers (mana abilities
// never reach the stack), Teamwork, CrewedBySource and Class-level triggers.
func classifyTapCombatTrigger(f *cards.Face, t *cards.Trigger) (sub string, ok bool) {
	if strings.EqualFold(t.ParamStr(cards.PKStatic), "True") || t.HasParam(cards.PKTeamwork) || t.HasParam(cards.PKClassBand) {
		return "", false
	}
	card := t.ParamStr(cards.PKValidCard)
	if filterHasToken(card, "CrewedBySource") {
		return "", false
	}
	switch t.ModeKind() {
	case cards.TriggerTaps, cards.TriggerTapAll:
		filter := card
		if t.ModeKind() == cards.TriggerTapAll {
			filter = t.ParamStr(cards.PKValidCards)
		}
		if tapSubjectFilter(f, t, filter) {
			return "trigger.tapped", true
		}
	case cards.TriggerAttacks:
		if bearerFilter(card) && !namesSelf(card) && !namesYouCtrl(card) {
			return "trigger.attacks-attached", true
		}
	case cards.TriggerBlocks:
		if (namesSelf(card) && f.IsCreature()) || creatureYouCtrl(card) || bearerFilter(card) {
			return "trigger.blocks", true
		}
	case cards.TriggerAttackerBlocked, cards.TriggerAttackerBlockedByCreature:
		blocker := t.ParamStr(cards.PKValidBlocker)
		if namesSelf(blocker) && f.IsCreature() {
			return "trigger.blocks", true
		}
		openBlocker := blocker == "" || strings.EqualFold(blocker, "Creature") || strings.EqualFold(blocker, "Card")
		if openBlocker && ((namesSelf(card) && f.IsCreature()) || creatureYouCtrl(card)) {
			return "trigger.blocked", true
		}
	case cards.TriggerAttackersDeclared:
		if opponentAttacksYou(t) {
			return "trigger.opponent-attacks", true
		}
	}
	return "", false
}

// tapSubjectFilter reports whether a Taps/TapAll filter names a creature the
// recipes can tap: the card itself, a creature or creature type its controller
// controls, the bearer of the card, or (for a tap by its controller) a
// creature an opponent controls.
func tapSubjectFilter(f *cards.Face, t *cards.Trigger, filter string) bool {
	switch {
	case bearerFilter(filter):
		return true
	case namesSelf(filter):
		return f.IsCreature()
	case creatureYouCtrl(filter):
		return true
	case filterHasToken(filter, "OppCtrl") && strings.EqualFold(t.ParamStr(cards.PKValidPlayer), "You"):
		return filterHasToken(filter, "Creature") && !nonCreatureType(filter)
	}
	return false
}

// bearerFilter reports a filter naming the creature the card is attached to.
// A Land filter (Fortify) never names a creature.
func bearerFilter(filter string) bool {
	if nonCreatureType(filter) {
		return false
	}
	return filterHasToken(filter, "AttachedBy") || filterHasToken(filter, "EquippedBy") || filterHasToken(filter, "EnchantedBy")
}

// creatureYouCtrl reports a filter for a creature (or creature type) the
// card's controller controls, as opposed to a land, artifact or other permanent.
func creatureYouCtrl(filter string) bool {
	return namesYouCtrl(filter) && !nonCreatureType(filter) && !namesSelf(filter)
}

// nonCreatureType reports a filter that names a non-creature permanent type.
func nonCreatureType(filter string) bool {
	for _, typ := range []string{"Land", "Artifact", "Enchantment", "Permanent", "Planeswalker", "Mountain", "Island", "Forest", "Swamp", "Plains"} {
		if filterHasToken(filter, typ) {
			return true
		}
	}
	return false
}

// opponentAttacksYou reports "whenever an opponent attacks you": an attack by
// p1 on p0, unconditioned by board or monarch state.
func opponentAttacksYou(t *cards.Trigger) bool {
	if !strings.EqualFold(t.ParamStr(cards.PKAttackedTarget), "You") {
		return false
	}
	if t.HasParam(cards.PKCondition) || t.HasParam(cards.PKCheckSVar) || t.HasParam(cards.PKCheckDefinedPlayer) || t.HasParam(cards.PKIsPresent) {
		return false
	}
	switch strings.ToLower(t.ParamStr(cards.PKAttackingPlayer)) {
	case "", "opponent", "player.opponent":
		return true
	}
	return false
}
