package cards

// GrantedKeywordAbility synthesizes the modeled activated ability represented
// by a derived keyword line. Unsupported keyword heads fail closed.
func GrantedKeywordAbility(line string) *SA {
	switch KeywordHead(line) {
	case "Cycling", "TypeCycling":
		return GrantedCyclingAbility(line)
	case "Saddle":
		return GrantedSaddleAbility(line)
	case "Crew":
		return GrantedCrewAbility(line)
	default:
		return nil
	}
}
