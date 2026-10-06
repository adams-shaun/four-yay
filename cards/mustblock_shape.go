package cards

import "strings"

// MustBlockNamedTargetShape certifies API forms whose blocker and attacker
// selectors can be resolved to concrete battlefield objects, plus Crashing
// Boars' exact choice-pool form (the defending player's resolving-time
// choice). Any other choice-pool or selector form stays fail-closed: keep
// coverage and resolution on the same parameter boundary.
func MustBlockNamedTargetShape(sa *SA) bool {
	if sa == nil || sa.API != "MustBlock" {
		return false
	}
	if strings.Compare(strings.TrimSpace(sa.ParamStr(PKChoices)), "Creature.untapped+DefenderCtrl") == 0 {
		if strings.Compare(strings.TrimSpace(sa.ParamStr(PKChooser)), "TriggeredDefendingPlayer") != 0 ||
			strings.TrimSpace(sa.ParamStr(PKDefinedAttacker)) != "" {
			return false
		}
		dur := strings.TrimSpace(sa.ParamStr(PKDuration))
		if dur != "" && strings.Compare(dur, "UntilEndOfCombat") != 0 {
			return false
		}
		for k := range sa.Params {
			switch k {
			case "Choices", "Chooser", "Duration":
			default:
				return false
			}
		}
		return true
	}
	// Every certified targeted grammar names creatures, and each value here
	// is a MustBlock ValidTgts$ the corpus prints. A missing ValidTgts is only
	// safe when the script supplies an explicit Defined set; otherwise
	// Defined's default-to-source behavior is a blocker, not a set.
	targets := strings.TrimSpace(sa.ParamStr(PKValidTgts))
	defined := strings.TrimSpace(sa.ParamStr(PKDefined))
	switch targets {
	case "", "Creature", "Creature.OppCtrl", "Creature.DefenderCtrl",
		"Creature.YouDontCtrl", "Creature.Artifact":
	default:
		return false
	}
	if targets == "" && defined == "" {
		return false
	}
	// Defined$ is not an arbitrary string: effects.Defined intentionally
	// falls back to the ability's source for unknown selectors. Certify only
	// the selector forms used by the corpus and resolved to concrete sets.
	if defined != "" {
		switch defined {
		case "ParentTarget", "Valid Creature.counters_GE1_MAGNET":
		default:
			return false
		}
	}
	attacker := strings.TrimSpace(sa.ParamStr(PKDefinedAttacker))
	switch attacker {
	case "", "TriggeredAttacker", "TriggeredAttackerLKICopy", "ParentTarget", "Valid Card.attacking":
	default:
		return false
	}
	// Other durations have not been checked against the continuous duty's
	// expiry semantics. No duration and this combat's duration are supported.
	switch strings.TrimSpace(sa.ParamStr(PKDuration)) {
	case "", "UntilEndOfCombat":
	default:
		return false
	}
	blockAll := strings.TrimSpace(sa.Params["BlockAllDefined"])
	if blockAll != "" && blockAll != "True" {
		return false
	}
	for k := range sa.Params {
		switch k {
		case "Defined", "DefinedAttacker", "ValidTgts", "Duration", "TgtPrompt", "Cost",
			"BlockAllDefined", "TargetMin", "TargetMax", "TargetUnique", "SpellDescription", "StackDescription",
			"PrecostDesc", "AILogic", "CheckSVar", "SVarCompare":
		default:
			return false
		}
	}
	return true
}
