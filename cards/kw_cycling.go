// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

func kwCycling(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	if has("A", k) {
		return
	}
	if sa := cyclingAbilitySA(param); sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

// cyclingAbilitySA builds the plain cycling ability body (CR 702.29a) the
// printed K:Cycling line expands to. It is shared by the printed expansion
// (kwCycling) and the granted route (cards.GrantedKeywordAbility) so the two
// constructions cannot drift.
func cyclingAbilitySA(cost string) *SA {
	sa, _ := parseSA("", "AB$ Draw | Cost$ "+cost+" Discard<1/CARDNAME> | ActivationZone$ Hand | NumCards$ 1 | Keyword$ Cycling | SpellDescription$ Cycling "+cost)
	return sa
}

// GrantedCyclingAbility is the cycling-only arm of GrantedKeywordAbility,
// kept so the cycling route's callers and shape test keep the exact
// contract they had: a line whose head is neither Cycling nor TypeCycling
// returns nil (fail closed).
func GrantedCyclingAbility(line string) *SA {
	switch KeywordHead(line) {
	case "Cycling", "TypeCycling":
		return GrantedKeywordAbility(line)
	}
	return nil
}

func init() { registerKeyword(kwCycling, "Cycling") }
