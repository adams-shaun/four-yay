package cards

import "strings"

// MustBlockNamedTargetShape certifies API forms whose blocker and attacker
// selectors can be resolved to concrete battlefield objects. Choice-pool forms
// remain fail-closed: they require a resolving-time choice, not a guessed target.
func MustBlockNamedTargetShape(sa *SA) bool {
	if sa == nil || sa.API != "MustBlock" {
		return false
	}
	// Every currently certified targeted grammar names creatures. A missing
	// ValidTgts is only safe when the script supplies an explicit Defined set;
	// otherwise Defined's default-to-source behavior is a blocker, not a set.
	targets := strings.TrimSpace(sa.ParamStr(PKValidTgts))
	defined := strings.TrimSpace(sa.ParamStr(PKDefined))
	switch targets {
	case "", "Creature", "Creature.OppCtrl", "Creature.DefenderCtrl", "Creature.YouCtrl",
		"Creature.YouDontCtrl", "Creature.Artifact", "Creature.stickeredWith PT+Other",
		"Creature.untapped+DefenderCtrl":
	default:
		return false
	}
	if targets == "" && defined == "" {
		return false
	}
	attacker := strings.TrimSpace(sa.ParamStr(PKDefinedAttacker))
	switch attacker {
	case "", "TriggeredAttacker", "TriggeredAttackerLKICopy", "ParentTarget", "Valid Card.attacking":
	default:
		return false
	}
	switch strings.TrimSpace(sa.ParamStr(PKDuration)) {
	case "", "UntilEndOfCombat":
	default:
		return false
	}
	for k := range sa.Params {
		switch k {
		case "Defined", "DefinedAttacker", "ValidTgts", "Duration", "TgtPrompt", "Cost",
			"TargetMin", "TargetMax", "TargetUnique", "SpellDescription", "StackDescription",
			"PrecostDesc", "AILogic", "CheckSVar", "SVarCompare":
		default:
			return false
		}
	}
	return true
}
