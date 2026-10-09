package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// classifyTapCombatTrigger admits the tap and combat-declaration shapes whose
// cause the level-B recipes can build from a combat the generator already
// plays: an attack (which taps the attacker), a block, an attachment, an
// untap step, or a tap spell. Gorge's own trigger matcher still decides, per
// card, whether the cause fires. It reports ok=false for Static$ True
// triggers (mana abilities never reach the stack), Teamwork, CrewedBySource
// and Class-level triggers -- except an AttackersDeclared Class body, whose
// granted trigger the shared class-level prelude raises before the cause.
func classifyTapCombatTrigger(f *cards.Face, t *cards.Trigger) (sub string, ok bool) {
	if strings.EqualFold(t.ParamStr(cards.PKStatic), "True") || t.HasParam(cards.PKTeamwork) {
		return "", false
	}
	card := t.ParamStr(cards.PKValidCard)
	if filterHasToken(card, "CrewedBySource") {
		return "", false
	}
	// AttackersDeclared is classified before the ClassBand guard: Party
	// Dude's level-3 granted body is an AttackersDeclared line whose
	// level-up prelude the shared triggerRecipe adds for every sub.
	if t.ModeKind() == cards.TriggerAttackersDeclared {
		if opponentAttacksYou(f, t) {
			return "trigger.opponent-attacks", true
		}
		if attacksYourOpponent(t) {
			return "trigger.attacks-opponent", true
		}
		return "", false
	}
	if t.HasParam(cards.PKClassBand) {
		return "", false
	}
	switch t.ModeKind() {
	case cards.TriggerUntapAll:
		// "Whenever you untap one or more permanents during your untap
		// step": the cause is p0's next untap step, with a tapped probe on
		// the battlefield so at least one permanent untaps.
		if vp := strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKValidPlayer))); vp == "" || vp == "you" {
			return "trigger.untap-all", true
		}
	case cards.TriggerAttached:
		// The engine's Attached matcher reads ValidSource$ (the attaching
		// object) and ValidTarget$ (the bearer). A line with no ValidTarget$
		// (Eriette's TargetRelativeToSource$) cannot fire and stays a gap.
		// Three source shapes are servable: the card itself (an
		// Equipment/Reconfigure), an Aura you control, and an Aura (any)
		// attaching to the source.
		if strings.TrimSpace(t.ParamStr(cards.PKValidTarget)) == "" || attachedTargetsTriggerRole(f, t) {
			break
		}
		switch {
		case strings.EqualFold(t.ParamStr(cards.PKValidSource), "Card.Self"):
			return "trigger.attached", true
		case strings.EqualFold(t.ParamStr(cards.PKValidSource), "Aura.YouCtrl"):
			return "trigger.attached", true
		case strings.EqualFold(t.ParamStr(cards.PKValidSource), "Aura"):
			return "trigger.attached", true
		}
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
		if namesSelf(card) && vehicleFace(f) {
			// A Vehicle's own "whenever this Vehicle blocks" line: the
			// cause crews it (its own crew/saddle activation) during the
			// opponent's attack, then declares it as a blocker.
			return "trigger.blocks-vehicle", true
		}
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
// p1 on p0, unconditioned by board or monarch state. The one CheckSVar shape
// admitted is the attacker-count gate (Tomik, Wielder of Law's
// Count$ValidAll Creature.attackingYouOrYourPWLKI with SVarCompare$ GE2): the
// recipe sends two attackers and the engine evaluates the count, so the SVar
// body must name the attacking-you count. Every other CheckSVar stays a gap,
// as does an AttackedTarget$ shape carrying one.
func opponentAttacksYou(f *cards.Face, t *cards.Trigger) bool {
	if t.HasParam(cards.PKCondition) || t.HasParam(cards.PKCheckDefinedPlayer) || t.HasParam(cards.PKIsPresent) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKAttackingPlayer))) {
	case "", "opponent", "player.opponent":
	default:
		return false
	}
	if cs := strings.TrimSpace(t.ParamStr(cards.PKCheckSVar)); cs != "" {
		if strings.TrimSpace(t.ParamStr(cards.PKAttackedTarget)) != "" {
			return false
		}
		return svarAttacksYou(f, cs)
	}
	return strings.EqualFold(t.ParamStr(cards.PKAttackedTarget), "You")
}

// svarAttacksYou reports an SVar body that counts the creatures attacking the
// trigger's controller (or their planeswalkers), the gate Tomik's recipe
// satisfies by attacking with the SVarCompare$ floor's worth of creatures.
func svarAttacksYou(f *cards.Face, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || f == nil {
		return false
	}
	return strings.Contains(strings.ToLower(f.SVars[name]), "attackingyou")
}

// attacksYourOpponent reports "whenever one or more of your opponents are
// attacked": p0's own attack on p1 (Party Dude's level-3 granted body).
// AttackedTarget$ must name exactly Opponent; a comma list or a qualifier
// (Opponent.lifeGTX, the multiplayer "another opponent" shapes) stays a gap,
// as does an AttackingPlayer$ that is not the controller.
func attacksYourOpponent(t *cards.Trigger) bool {
	if !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKAttackedTarget)), "Opponent") {
		return false
	}
	if t.HasParam(cards.PKCondition) || t.HasParam(cards.PKCheckSVar) || t.HasParam(cards.PKCheckDefinedPlayer) || t.HasParam(cards.PKIsPresent) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(t.ParamStr(cards.PKAttackingPlayer))) {
	case "", "you", "player":
		return true
	}
	return false
}

// vehicleFace reports a face whose own card can be crewed or saddled into a
// creature: the Vehicle type, or a Crew/Saddle keyword.
func vehicleFace(f *cards.Face) bool {
	for _, typ := range f.Types {
		if strings.EqualFold(typ, "Vehicle") {
			return true
		}
	}
	for _, kw := range f.Keywords {
		lower := strings.ToLower(kw)
		if strings.HasPrefix(lower, "crew") || strings.HasPrefix(lower, "saddle") {
			return true
		}
	}
	return false
}

// attachedTargetsTriggerRole reports an Attached trigger whose effect target
// spec names TriggeredTarget (Blade of Shared Souls' "another target creature
// you control"). The push-time target ask builds its spec context from the
// trigger's stack object, which does not carry the TriggerTarget role, so
// the spec fails closed, the trigger is never put on the stack, and no cause
// can serve it; it stays a gap until that ask binds the role.
func attachedTargetsTriggerRole(f *cards.Face, t *cards.Trigger) bool {
	sa := cards.ResolveSVar(f.SVars, t.ParamStr(cards.PKExecute))
	if sa == nil {
		return false
	}
	return strings.Contains(strings.ToLower(sa.ParamStr(cards.PKValidTgts)), "triggeredtarget")
}
