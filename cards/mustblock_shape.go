package cards

import "strings"

// MustBlockNamedTargetShape is the only API shape whose duty the resolver can
// currently represent: one mandatory chosen blocker and the triggered attacker.
// Keep coverage and resolution on the same fail-closed parameter boundary.
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
	if strings.TrimSpace(sa.ParamStr(PKDefinedAttacker)) != "TriggeredAttacker" {
		return false
	}
	// The resolver creates duties only for battlefield objects. In particular,
	// player and mixed alternatives can be selected but are silently skipped.
	// Certify only the creature selectors established by this API's tests;
	// other qualifiers and multi-target grammars need their own audit.
	switch strings.TrimSpace(sa.ParamStr(PKValidTgts)) {
	case "Creature", "Creature.OppCtrl":
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
	for k := range sa.Params {
		switch k {
		case "DefinedAttacker", "ValidTgts", "Duration", "TgtPrompt":
		default:
			return false
		}
	}
	return true
}
