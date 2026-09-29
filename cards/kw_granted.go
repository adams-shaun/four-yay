// The granted-keyword synthesizer (CR 613.1f). A layer-6 AddKeyword$ grant
// lands an EXPANDED keyword line in a face's DERIVED keyword list, but the
// link-time expansion (cards/keywords.go expandKeywords) only ever reads a
// face's PRINTED keyword list, so no ability is minted for the grant. This
// file is the one dispatcher every granted-keyword read site calls: it
// synthesizes the ability body for the four heads that mint an activated
// ability (Cycling, TypeCycling, Saddle, Crew) from the line alone, sharing
// each body builder with the printed expansion so the two cannot drift.

package cards

import "strings"

// grantedKeywordHeads is the set of keyword heads GrantedKeywordAbility can
// synthesize an activated ability for. Every other head returns nil (fail
// closed): the expansion of a keyword like Equip, Reconfigure or LevelUp
// mints an ability with its own zone/target/alt-cost design, and handing back
// a wrong body would be worse than handing back none.
func grantedKeywordHeads(head string) bool {
	switch head {
	case "Cycling", "TypeCycling", "Saddle", "Crew":
		return true
	}
	return false
}

// GrantedKeywordAbility synthesizes the activated ability a keyword LINE
// grants. The line is the full "Head:param..." text exactly as it sits in a
// face's keyword list ("Cycling:2", "Crew:1", "Saddle:2") or in a layer-6
// AddKeyword$ grant's derived entry ("Cycling:1 U", "TypeCycling:Sliver:3"):
// the same shape the printed expanders parse, so the granted body is
// byte-identical to the printed one's. The returned SA carries KeywordLine =
// the line, the same tag the printed expansion sets. A line whose head mints
// no activated ability -- or whose shape the construction cannot model --
// returns nil (the totality stance every synthesizer takes), so a caller
// fails closed to no ability rather than a wrong one.
func GrantedKeywordAbility(line string) *SA {
	head := KeywordHead(line)
	if !grantedKeywordHeads(head) {
		return nil
	}
	param := ""
	if j := strings.IndexByte(line, ':'); j >= 0 {
		param = strings.TrimSpace(line[j+1:])
	}
	var sa *SA
	switch head {
	case "Cycling":
		sa = cyclingAbilitySA(param)
	case "TypeCycling":
		// TypeCycling's param is "<type>:<cost>[...]" -- the same split the
		// printed expander (kwTypeCycling) runs.
		typeSpec, rest, _ := strings.Cut(param, ":")
		cost, _, _ := strings.Cut(rest, ":")
		sa = typeCyclingAbilitySA(strings.TrimSpace(typeSpec), strings.TrimSpace(cost))
	case "Saddle":
		// Saddle's param is "<N>[:...]"; a trailing field is display prose
		// and stays dropped, exactly as the printed expander drops it.
		sa = saddleAbilitySA(strings.TrimSpace(strings.Split(param, ":")[0]))
	case "Crew":
		n, riders := crewParamParts(param)
		sa = crewAbilitySA(n, riders)
	}
	if sa != nil {
		sa.Params["KeywordLine"] = line
	}
	return sa
}
