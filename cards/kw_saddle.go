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
	n := strings.TrimSpace(strings.Split(param, ":")[0])
	if n == "" {
		n = "1"
	}
	if sa := saddleAbilitySA(n); sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

func saddleAbilitySA(n string) *SA {
	saStr := "AB$ AlterAttribute | Cost$ tapXType<Any/Creature.Other+withTotalPowerGE" + n +
		"> | Defined$ Self | Attributes$ Saddled | SorcerySpeed$ True | Keyword$ Saddle" +
		" | SpellDescription$ Saddle " + n + " (Tap any number of other untapped creatures you control with total power " + n + " or greater: This creature becomes saddled until end of turn.)"
	sa, _ := parseSA("", saStr)
	return sa
}

// GrantedSaddleAbility synthesizes the activated ability for a derived Saddle line.
func GrantedSaddleAbility(line string) *SA {
	if KeywordHead(line) != "Saddle" {
		return nil
	}
	param := ""
	if j := strings.IndexByte(line, ':'); j >= 0 {
		param = strings.TrimSpace(line[j+1:])
	}
	n := strings.TrimSpace(strings.Split(param, ":")[0])
	if n == "" {
		n = "1"
	}
	sa := saddleAbilitySA(n)
	if sa != nil {
		sa.Params["KeywordLine"] = line
	}
	return sa
}

func init() { registerKeyword(kwSaddle, "Saddle") }
