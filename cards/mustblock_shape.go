package cards

import "strings"

// MustBlockNamedTargetShape is the only API shape whose duty the resolver can
// currently represent: one mandatory chosen blocker and the triggered attacker.
// Keep coverage and resolution on the same fail-closed parameter boundary.
func MustBlockNamedTargetShape(sa *SA) bool {
	if sa == nil || sa.API != "MustBlock" {
		return false
	}
	attacker := strings.TrimSpace(sa.ParamStr(PKDefinedAttacker))
	valid := strings.TrimSpace(sa.ParamStr(PKValidTgts))
	duration := strings.TrimSpace(sa.ParamStr(PKDuration))
	fighterClass := attacker == "TriggeredAttackerLKICopy" && valid == "Creature" &&
		strings.TrimSpace(sa.ParamStr(PKTargetMin)) == "0" &&
		strings.TrimSpace(sa.ParamStr(PKTargetMax)) == "1" &&
		strings.TrimSpace(sa.Params["BlockAllDefined"]) == "True" && duration == "UntilEndOfCombat"
	if attacker != "TriggeredAttacker" && !fighterClass {
		return false
	}
	// The resolver creates duties only for battlefield objects. In particular,
	// player and mixed alternatives can be selected but are silently skipped.
	// Certify only the creature selectors established by this API's tests.
	if fighterClass {
		if valid != "Creature" {
			return false
		}
	} else if valid != "Creature" && valid != "Creature.OppCtrl" {
		return false
	}
	// Other durations have not been checked against the continuous duty's
	// expiry semantics. No duration and this combat's duration are supported.
	if !fighterClass && duration != "" && duration != "UntilEndOfCombat" {
		return false
	}
	for k := range sa.Params {
		switch k {
		case "DefinedAttacker", "ValidTgts", "Duration", "TgtPrompt":
		case "TargetMin", "TargetMax", "BlockAllDefined":
			if !fighterClass {
				return false
			}
		default:
			return false
		}
	}
	return true
}
