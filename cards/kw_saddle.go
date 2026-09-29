package cards

import (
	"strings"
)

// kwSaddle expands K:Saddle:<N> (CR 702.171) into the activated ability that
// taps other creatures with total power N or greater and marks the Mount
// saddled until end of turn.
func kwSaddle(f *Face, _ int, k, _, param string, has func(kind, line string) bool) {
	if has("A", k) {
		return
	}
	if sa := saddleAbilitySA(strings.TrimSpace(strings.Split(param, ":")[0])); sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

// saddleAbilitySA builds the saddle ability body (CR 702.171) the printed
// K:Saddle:<N> line expands to. It is shared by the printed expansion
// (kwSaddle) and the granted route (GrantedKeywordAbility) so the two
// constructions cannot drift. A blank count defaults to "1".
func saddleAbilitySA(n string) *SA {
	if n == "" {
		n = "1"
	}
	saStr := "AB$ AlterAttribute | Cost$ tapXType<Any/Creature.Other+withTotalPowerGE" + n +
		"> | Defined$ Self | Attributes$ Saddled | SorcerySpeed$ True | Keyword$ Saddle" +
		" | SpellDescription$ Saddle " + n + " (Tap any number of other untapped creatures you control with total power " + n + " or greater: This creature becomes saddled until end of turn.)"
	sa, _ := parseSA("", saStr)
	return sa
}

func init() { registerKeyword(kwSaddle, "Saddle") }
