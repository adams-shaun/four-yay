package cost

import (
	"regexp"
	"testing"
)

// MatcherCase pairs one hand-compiled cost-token matcher with the regexp
// source it replaced (the grammar const in parse.go).
type MatcherCase struct {
	Name    string
	Grammar string
	Match   func(sym string) ([6]string, bool)
	re      *regexp.Regexp
}

func wrapMatcher(f func(costTok) (groups, bool)) func(string) ([6]string, bool) {
	return func(sym string) ([6]string, bool) {
		g, ok := f(splitTok(sym))
		return g, ok
	}
}

// matcherCases is every anchored token matcher ParseCost and ParseUnlessCost
// call, with its reference grammar.
var matcherCases = func() []MatcherCase {
	cs := []MatcherCase{
		{Name: "nonManaCost", Grammar: nonManaCost, Match: wrapMatcher(matchNonManaCost)},
		{Name: "drawDynCost", Grammar: drawDynCost, Match: wrapMatcher(matchDrawDynCost)},
		{Name: "sacXCost", Grammar: sacXCost, Match: wrapMatcher(matchSacXCost)},
		{Name: "exileCost", Grammar: exileCost, Match: wrapMatcher(matchExileCost)},
		{Name: "exileFromTopCost", Grammar: exileFromTopCost, Match: wrapMatcher(matchExileFromTopCost)},
		{Name: "addCounterCost", Grammar: addCounterCost, Match: wrapMatcher(matchAddCounterCost)},
		{Name: "addSelfCounterCost", Grammar: addSelfCounterCost, Match: wrapMatcher(matchAddSelfCounterCost)},
		{Name: "exertCost", Grammar: exertCost, Match: wrapMatcher(matchExertCost)},
		{Name: "lifeCost", Grammar: lifeCost, Match: wrapMatcher(matchLifeCost)},
		{Name: "choiceCost", Grammar: choiceCost, Match: wrapMatcher(matchChoiceCost)},
		{Name: "choiceCostRevealOrChoose", Grammar: choiceCostRevealOrChoose, Match: wrapMatcher(matchRevealOrChooseCost)},
		{Name: "revealChosenCost", Grammar: revealChosenCost, Match: wrapMatcher(matchRevealChosenCost)},
		{Name: "dynTapCost", Grammar: dynTapCost, Match: wrapMatcher(matchDynTapCost)},
		{Name: "untapYTypeCost", Grammar: untapYTypeCost, Match: wrapMatcher(matchUntapYTypeCost)},
		{Name: "blightCost", Grammar: blightCost, Match: wrapMatcher(matchBlightCost)},
		{Name: "payEnergyCost", Grammar: payEnergyCost, Match: wrapMatcher(matchPayEnergyCost)},
		{Name: "returnCost", Grammar: returnCost, Match: wrapMatcher(matchReturnCost)},
		{Name: "putCardToLibCost", Grammar: putCardToLibCost, Match: wrapMatcher(matchPutCardToLibCost)},
		{Name: "exileBattlefieldCost", Grammar: exileBattlefieldCost, Match: wrapMatcher(matchExileBattlefieldCost)},
		{Name: "exiledMoveToGraveCost", Grammar: exiledMoveToGraveCost, Match: wrapMatcher(matchExiledMoveToGraveCost)},
		{Name: "millCost", Grammar: millCost, Match: wrapMatcher(matchMillCost)},
		{Name: "evidenceCost", Grammar: evidenceCost, Match: wrapMatcher(matchEvidenceCost)},
		{Name: "payLifeXCost", Grammar: payLifeXCost, Match: wrapMatcher(matchPayLifeXCost)},
		{Name: "subCounterCost", Grammar: subCounterCost, Match: wrapMatcher(matchSubCounterCost)},
		{Name: "removeAnyCounterCost", Grammar: removeAnyCounterCost, Match: wrapMatcher(matchRemoveAnyCounterCost)},
		{Name: "damageYouCost", Grammar: damageYouCost, Match: wrapMatcher(matchDamageYouCost)},
		{Name: "gainLifeCost", Grammar: gainLifeCost, Match: wrapMatcher(matchGainLifeCost)},
		{Name: "rollDiceCost", Grammar: rollDiceCost, Match: wrapMatcher(matchRollDiceCost)},
		{Name: "xMinCost", Grammar: xMinCost, Match: wrapMatcher(matchXMinCost)},
		{Name: "waterbendCost", Grammar: waterbendCost, Match: wrapMatcher(matchWaterbendCost)},
	}
	for i := range cs {
		cs[i].re = regexp.MustCompile(cs[i].Grammar)
	}
	return cs
}()

var groupPowerFloorRE = regexp.MustCompile(groupPowerFloor)

// MatcherCount is the number of token matchers CheckToken compares.
func MatcherCount() int { return len(matcherCases) }

// CheckToken fails tb for every matcher whose answer on sym differs from its
// reference regexp's FindStringSubmatch (match or not, and every capture
// group), and for findGroupPowerFloor against the unanchored
// withTotalPowerGE regexp's FindStringSubmatchIndex.
func CheckToken(tb testing.TB, sym string) {
	tb.Helper()
	for _, mc := range matcherCases {
		want := mc.re.FindStringSubmatch(sym)
		got, ok := mc.Match(sym)
		if ok != (want != nil) {
			tb.Errorf("%s(%q): matched=%v, regexp matched=%v (%q)", mc.Name, sym, ok, want != nil, want)
			continue
		}
		if !ok {
			continue
		}
		for i := range got {
			w := ""
			if i < len(want) {
				w = want[i]
			}
			if got[i] != w {
				tb.Errorf("%s(%q): group %d = %q, regexp %q (all: %q vs %q)", mc.Name, sym, i, got[i], w, got, want)
				break
			}
		}
	}
	want := groupPowerFloorRE.FindStringSubmatchIndex(sym)
	s, e, ds, de, ok := findGroupPowerFloor(sym)
	if ok != (want != nil) {
		tb.Errorf("findGroupPowerFloor(%q): matched=%v, regexp %v", sym, ok, want)
	} else if ok && (s != want[0] || e != want[1] || ds != want[2] || de != want[3]) {
		tb.Errorf("findGroupPowerFloor(%q) = %d,%d,%d,%d, regexp %v", sym, s, e, ds, de, want)
	}
}

// Mutations returns deterministic boundary variants of a token: the
// truncations, field doublings and substitutions most likely to separate a
// hand-compiled grammar from its regexp.
func Mutations(sym string) []string {
	out := []string{sym + ">", sym + "/", "<" + sym, sym + "<>", "X" + sym}
	if len(sym) > 0 {
		out = append(out, sym[1:], sym[:len(sym)-1], sym[:len(sym)-1]+"/>", sym[:len(sym)-1]+"//>")
	}
	for i := 0; i < len(sym); i++ {
		switch c := sym[i]; {
		case c == '/':
			out = append(out, sym[:i]+"//"+sym[i+1:], sym[:i]+sym[i+1:], sym[:i]+"/>"+sym[i+1:])
		case c == '<':
			out = append(out, sym[:i]+"<<"+sym[i+1:], sym[:i]+"<X"+sym[i:], sym[:i+1]+"-"+sym[i+1:])
		case c >= '0' && c <= '9':
			out = append(out, sym[:i]+"X"+sym[i+1:], sym[:i]+"X"+sym[i:]+"+", sym[:i]+"a"+sym[i+1:])
		}
	}
	return out
}
