package cards

import "strings"

// MustBlockNamedTargetShape is the only API shape whose duty the resolver can
// currently represent: one mandatory chosen blocker and the triggered attacker.
// Keep coverage and resolution on the same fail-closed parameter boundary.
func MustBlockNamedTargetShape(sa *SA) bool {
	if sa == nil || sa.API != "MustBlock" || strings.TrimSpace(sa.ParamStr(PKDefinedAttacker)) != "TriggeredAttacker" || strings.TrimSpace(sa.ParamStr(PKValidTgts)) == "" {
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
