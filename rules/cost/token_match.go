package cost

import "strings"

// This file is the cost-token grammar ParseCost and ParseUnlessCost read,
// compiled by hand from the regexps that used to spell it (operator perf
// rule: hot paths compile strings, never run a regexp engine). Every matcher
// is the exact language of the regexp named in its doc comment, and returns
// that regexp's FindStringSubmatch groups in a fixed-size array (index 0 the
// whole token, then the capture groups in order; an unmatched optional group
// is ""), so the parser bodies read m[1], m[2], ... exactly as before. The
// equivalence is held by token_match_test.go (every corpus token, a
// mutation sweep and a fuzz target against the original regexps).
//
// The regexp sources themselves stay in parse.go as documentation: each
// is a const under the name the compiled regexp used to carry (nonManaCost,
// lifeCost, ...), with its doc comment, and the equivalence tests compile
// those consts as the reference grammar. Production code never compiles
// them.
//
// Matching allocates nothing: the groups are substrings of the token.

// groups is one matcher's capture groups (see the file comment).
type groups [6]string

// costHead is a cost token's head compiled to an enum: the text before its
// first '<', looked up once per token in costHeadNames. Every matcher
// dispatches on it instead of comparing strings.
type costHead uint8

const (
	hNone costHead = iota
	hAddCounter
	hBehold
	hBeholdExile
	hBlight
	hCollectEvidence
	hDamageYou
	hDiscard
	hDraw
	hExert
	hExile
	hExileAnyGrave
	hExileCtrlOrGrave
	hExileFromGrave
	hExileFromHand
	hExileFromTop
	hExiledMoveToGrave
	hGainLife
	hMill
	hPayEnergy
	hPayLife
	hPutCardToLibFromBattlefield
	hPutCardToLibFromGrave
	hPutCardToLibFromHand
	hRemoveAnyCounter
	hReturn
	hReveal
	hRevealChosen
	hRevealOrChoose
	hRollDice
	hSac
	hSubCounter
	hWaterbend
	hTapXType
	hUntapYType
	numCostHeads
)

// costHeadNames spells every head the grammar knows, indexed by costHead and
// kept in byte order (TestCostHeadTableIsSorted) for lookupCostHead's binary
// search.
var costHeadNames = [numCostHeads]string{
	hAddCounter:                  "AddCounter",
	hBehold:                      "Behold",
	hBeholdExile:                 "BeholdExile",
	hBlight:                      "Blight",
	hCollectEvidence:             "CollectEvidence",
	hDamageYou:                   "DamageYou",
	hDiscard:                     "Discard",
	hDraw:                        "Draw",
	hExert:                       "Exert",
	hExile:                       "Exile",
	hExileAnyGrave:               "ExileAnyGrave",
	hExileCtrlOrGrave:            "ExileCtrlOrGrave",
	hExileFromGrave:              "ExileFromGrave",
	hExileFromHand:               "ExileFromHand",
	hExileFromTop:                "ExileFromTop",
	hExiledMoveToGrave:           "ExiledMoveToGrave",
	hGainLife:                    "GainLife",
	hMill:                        "Mill",
	hPayEnergy:                   "PayEnergy",
	hPayLife:                     "PayLife",
	hPutCardToLibFromBattlefield: "PutCardToLibFromBattlefield",
	hPutCardToLibFromGrave:       "PutCardToLibFromGrave",
	hPutCardToLibFromHand:        "PutCardToLibFromHand",
	hRemoveAnyCounter:            "RemoveAnyCounter",
	hReturn:                      "Return",
	hReveal:                      "Reveal",
	hRevealChosen:                "RevealChosen",
	hRevealOrChoose:              "RevealOrChoose",
	hRollDice:                    "RollDice",
	hSac:                         "Sac",
	hSubCounter:                  "SubCounter",
	hWaterbend:                   "Waterbend",
	hTapXType:                    "tapXType",
	hUntapYType:                  "untapYType",
}

// lookupCostHead compiles head to its costHead, hNone for a head the grammar
// does not know.
func lookupCostHead(head string) costHead {
	lo, hi := int(hNone)+1, int(numCostHeads)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if costHeadNames[mid] < head {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < int(numCostHeads) && costHeadNames[lo] == head {
		return costHead(lo)
	}
	return hNone
}

// costTok is one cost token split once at its first '<'. For the
// Head<inner> shape every head regexp shares, head is the text before the
// '<' and inner the text between it and the token's final '>'; angle is
// false for any token not of that shape: no '<', no closing '>' as the last
// byte, or a '>' inside inner (every grammar field excludes '>', so such a
// token matches no head).
type costTok struct {
	sym, head, inner string
	id               costHead
	angle            bool
}

func splitTok(sym string) costTok {
	t := costTok{sym: sym}
	i := strings.IndexByte(sym, '<')
	if i < 0 || len(sym) < i+2 || sym[len(sym)-1] != '>' {
		return t
	}
	inner := sym[i+1 : len(sym)-1]
	if strings.IndexByte(inner, '>') >= 0 {
		return t
	}
	t.head, t.inner, t.angle = sym[:i], inner, true
	t.id = lookupCostHead(t.head)
	return t
}

// isDigits reports whether s is a non-empty run of ASCII digits (\d+).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isXOrDigits is (X|\d+), equivalently ([0-9]+|X) and (\d+|X).
func isXOrDigits(s string) bool { return s == "X" || isDigits(s) }

// isSignedDigits is -?\d+.
func isSignedDigits(s string) bool {
	if len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	return isDigits(s)
}

// isCounterCount is (X\d+\+|X|\d+).
func isCounterCount(s string) bool {
	if s == "X" || isDigits(s) {
		return true
	}
	return len(s) >= 3 && s[0] == 'X' && s[len(s)-1] == '+' && isDigits(s[1:len(s)-1])
}

// isSVarName is [A-Za-z][A-Za-z0-9]*.
func isSVarName(s string) bool {
	if s == "" || !isASCIILetter(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isASCIILetter(s[i]) && (s[i] < '0' || s[i] > '9') {
			return false
		}
	}
	return true
}

// isCounterKind is [A-Za-z0-9_]+.
func isCounterKind(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !isASCIILetter(c) && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// countSpecDesc reads the inner shape (C)/([^/>]+)(?:/([^>]*))? shared by
// most heads, where count is C's language: the count runs to the first '/',
// the spec to the next '/' (non-empty), and whatever follows that '/' is the
// description ("" when there is none).
func countSpecDesc(inner string, count func(string) bool) (n, spec, desc string, ok bool) {
	n, rest, found := strings.Cut(inner, "/")
	if !found || !count(n) {
		return "", "", "", false
	}
	spec, desc, _ = strings.Cut(rest, "/")
	if spec == "" {
		return "", "", "", false
	}
	return n, spec, desc, true
}

// countDesc reads the inner shape (C)(?:/([^>]*))?.
func countDesc(inner string, count func(string) bool) (n, desc string, ok bool) {
	n, desc, _ = strings.Cut(inner, "/")
	if !count(n) {
		return "", "", false
	}
	return n, desc, true
}

// matchHeadCountSpecDesc is ^Head<(C)/([^/>]+)(?:/([^>]*))?>$ for one fixed
// Head: groups [sym, count, spec, desc].
func matchHeadCountSpecDesc(t costTok, head costHead, count func(string) bool) (groups, bool) {
	if !t.angle || t.id != head {
		return groups{}, false
	}
	n, spec, desc, ok := countSpecDesc(t.inner, count)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, n, spec, desc}, true
}

// matchNonManaCost is ^(Sac|SubCounter|Discard|Draw)<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchNonManaCost(t costTok) (groups, bool) {
	if !t.angle || (t.id != hSac && t.id != hSubCounter && t.id != hDiscard && t.id != hDraw) {
		return groups{}, false
	}
	n, spec, desc, ok := countSpecDesc(t.inner, isDigits)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, t.head, n, spec, desc}, true
}

// matchDrawDynCost is ^Draw<([A-Za-z][A-Za-z0-9]*)/([^/>]+)(?:/([^>]*))?>$.
func matchDrawDynCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hDraw, isSVarName)
}

// matchSacXCost is ^Sac<X/([^/>]+)(?:/([^>]*))?>$: groups [sym, spec, desc].
func matchSacXCost(t costTok) (groups, bool) {
	m, ok := matchHeadCountSpecDesc(t, hSac, func(s string) bool { return s == "X" })
	if !ok {
		return groups{}, false
	}
	return groups{m[0], m[2], m[3]}, true
}

// matchExileCost is
// ^Exile(FromHand|FromGrave|AnyGrave)<(X|\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchExileCost(t costTok) (groups, bool) {
	if !t.angle || (t.id != hExileFromHand && t.id != hExileFromGrave && t.id != hExileAnyGrave && t.id != hExileCtrlOrGrave) {
		return groups{}, false
	}
	n, spec, desc, ok := countSpecDesc(t.inner, isXOrDigits)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, t.head[len("Exile"):], n, spec, desc}, true
}

// matchExileFromTopCost is ^ExileFromTop<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchExileFromTopCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hExileFromTop, isDigits)
}

// matchAddCounterCost is ^AddCounter<(\d+)/(LOYALTY)(?:/([^>]*))?>$.
func matchAddCounterCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hAddCounter {
		return groups{}, false
	}
	n, rest, found := strings.Cut(t.inner, "/")
	if !found || !isDigits(n) {
		return groups{}, false
	}
	kind, desc, _ := strings.Cut(rest, "/")
	if kind != "LOYALTY" {
		return groups{}, false
	}
	return groups{t.sym, n, kind, desc}, true
}

// matchAddSelfCounterCost is ^AddCounter<(\d+)/([A-Za-z0-9_]+)>$.
func matchAddSelfCounterCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hAddCounter {
		return groups{}, false
	}
	n, kind, found := strings.Cut(t.inner, "/")
	if !found || !isDigits(n) || !isCounterKind(kind) {
		return groups{}, false
	}
	return groups{t.sym, n, kind}, true
}

// matchExertCost is ^Exert<1/(?:CARDNAME|NICKNAME)(?:/([^>]*))?>$: groups
// [sym, desc].
func matchExertCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hExert {
		return groups{}, false
	}
	rest, found := strings.CutPrefix(t.inner, "1/")
	if !found {
		return groups{}, false
	}
	name, desc, _ := strings.Cut(rest, "/")
	if name != "CARDNAME" && name != "NICKNAME" {
		return groups{}, false
	}
	return groups{t.sym, desc}, true
}

// matchLifeCost is ^PayLife<(\d+)>$.
func matchLifeCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hPayLife || !isDigits(t.inner) {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// matchPayLifeXCost is ^PayLife<X>$: groups [sym].
func matchPayLifeXCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hPayLife || t.inner != "X" {
		return groups{}, false
	}
	return groups{t.sym}, true
}

// matchChoiceCost is
// ^(Reveal|Behold|BeholdExile|tapXType)<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchChoiceCost(t costTok) (groups, bool) {
	if !t.angle || (t.id != hReveal && t.id != hBehold && t.id != hBeholdExile && t.id != hTapXType) {
		return groups{}, false
	}
	n, spec, desc, ok := countSpecDesc(t.inner, isDigits)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, t.head, n, spec, desc}, true
}

// matchRevealOrChooseCost is ^RevealOrChoose<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchRevealOrChooseCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hRevealOrChoose, isDigits)
}

// matchRevealChosenCost is ^RevealChosen<(Player|Type)(?:/([^>]*))?>$.
func matchRevealChosenCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hRevealChosen {
		return groups{}, false
	}
	n, desc, ok := countDesc(t.inner, func(s string) bool { return s == "Player" || s == "Type" })
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, n, desc}, true
}

// matchDynTapCost is ^tapXType<(X|Any)/([^/>]+)(?:/([^>]*))?>$.
func matchDynTapCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hTapXType, func(s string) bool { return s == "X" || s == "Any" })
}

// matchUntapYTypeCost is ^untapYType<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchUntapYTypeCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hUntapYType, isDigits)
}

// matchBlightCost is ^Blight<(\d+|X)>$.
func matchBlightCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hBlight || !isXOrDigits(t.inner) {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// matchPayEnergyCost is ^PayEnergy<([0-9]+|X)(?:/([^>]*))?>$.
func matchPayEnergyCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hPayEnergy {
		return groups{}, false
	}
	n, desc, ok := countDesc(t.inner, isXOrDigits)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, n, desc}, true
}

// matchReturnCost is ^Return<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchReturnCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hReturn, isDigits)
}

// matchPutCardToLibCost is
// ^PutCardToLibFrom(Hand|Grave|Battlefield)<(\d+)/(-?\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchPutCardToLibCost(t costTok) (groups, bool) {
	if !t.angle || (t.id != hPutCardToLibFromHand && t.id != hPutCardToLibFromGrave && t.id != hPutCardToLibFromBattlefield) {
		return groups{}, false
	}
	n, rest, found := strings.Cut(t.inner, "/")
	if !found || !isDigits(n) {
		return groups{}, false
	}
	pos, specDesc, found := strings.Cut(rest, "/")
	if !found || !isSignedDigits(pos) {
		return groups{}, false
	}
	spec, desc, _ := strings.Cut(specDesc, "/")
	if spec == "" {
		return groups{}, false
	}
	return groups{t.sym, t.head[len("PutCardToLibFrom"):], n, pos, spec, desc}, true
}

// matchExileBattlefieldCost is ^Exile<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchExileBattlefieldCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hExile, isDigits)
}

// matchExiledMoveToGraveCost is
// ^ExiledMoveToGrave<(\d+)/([^/>]+)(?:/([^>]*))?>$.
func matchExiledMoveToGraveCost(t costTok) (groups, bool) {
	return matchHeadCountSpecDesc(t, hExiledMoveToGrave, isDigits)
}

// matchMillCost is ^Mill<(\d+)>$.
func matchMillCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hMill || !isDigits(t.inner) {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// matchEvidenceCost is ^CollectEvidence<([^>]+)>$.
func matchEvidenceCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hCollectEvidence || t.inner == "" {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// matchCounterRemoval is ^Head<(X\d+\+|X|\d+)/([^/>]+)(?:/([^/>]+))?(?:/([^>]*))?>$
// for one fixed Head: groups [sym, count, kind, target, desc]. The optional
// third field is taken whenever it is non-empty (the regexp's greedy
// optional group); an empty one leaves the whole remainder to the
// description.
func matchCounterRemoval(t costTok, head costHead) (groups, bool) {
	if !t.angle || t.id != head {
		return groups{}, false
	}
	n, rest, found := strings.Cut(t.inner, "/")
	if !found || !isCounterCount(n) {
		return groups{}, false
	}
	kind, tail, found := strings.Cut(rest, "/")
	if kind == "" {
		return groups{}, false
	}
	if !found {
		return groups{t.sym, n, kind}, true
	}
	target, desc, _ := strings.Cut(tail, "/")
	if target == "" {
		return groups{t.sym, n, kind, "", tail}, true
	}
	return groups{t.sym, n, kind, target, desc}, true
}

// matchSubCounterCost is matchCounterRemoval for SubCounter.
func matchSubCounterCost(t costTok) (groups, bool) { return matchCounterRemoval(t, hSubCounter) }

// matchRemoveAnyCounterCost is matchCounterRemoval for RemoveAnyCounter.
func matchRemoveAnyCounterCost(t costTok) (groups, bool) {
	return matchCounterRemoval(t, hRemoveAnyCounter)
}

// matchDamageYouCost is ^DamageYou<(\d+)(?:/([^>]*))?>$.
func matchDamageYouCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hDamageYou {
		return groups{}, false
	}
	n, desc, ok := countDesc(t.inner, isDigits)
	if !ok {
		return groups{}, false
	}
	return groups{t.sym, n, desc}, true
}

// matchGainLifeCost is ^GainLife<(\d+)/(Player[^/>]+)(?:/([^>]*))?>$.
func matchGainLifeCost(t costTok) (groups, bool) {
	m, ok := matchHeadCountSpecDesc(t, hGainLife, isDigits)
	if !ok || len(m[2]) <= len("Player") || !strings.HasPrefix(m[2], "Player") {
		return groups{}, false
	}
	return m, true
}

// matchRollDiceCost is ^RollDice<([^>]*)>$.
func matchRollDiceCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hRollDice {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// matchXMinCost is ^XMin(\d+)$.
func matchXMinCost(t costTok) (groups, bool) {
	n, found := strings.CutPrefix(t.sym, "XMin")
	if !found || !isDigits(n) {
		return groups{}, false
	}
	return groups{t.sym, n}, true
}

// matchWaterbendCost is ^Waterbend<(X|\d+)>$.
func matchWaterbendCost(t costTok) (groups, bool) {
	if !t.angle || t.id != hWaterbend || !isXOrDigits(t.inner) {
		return groups{}, false
	}
	return groups{t.sym, t.inner}, true
}

// groupPowerFloorWord is the withTotalPowerGE<N> group predicate's literal.
const groupPowerFloorWord = "withTotalPowerGE"

// findGroupPowerFloor is the unanchored withTotalPowerGE(\d+): the leftmost
// occurrence of the word followed by at least one digit, with its digits
// taken greedily. It returns the regexp's FindStringSubmatchIndex pairs
// (whole match, then the digit group), or ok false.
func findGroupPowerFloor(spec string) (start, end, digitsStart, digitsEnd int, ok bool) {
	for from := 0; from <= len(spec); {
		i := strings.Index(spec[from:], groupPowerFloorWord)
		if i < 0 {
			return 0, 0, 0, 0, false
		}
		start = from + i
		digitsStart = start + len(groupPowerFloorWord)
		digitsEnd = digitsStart
		for digitsEnd < len(spec) && spec[digitsEnd] >= '0' && spec[digitsEnd] <= '9' {
			digitsEnd++
		}
		if digitsEnd > digitsStart {
			return start, digitsEnd, digitsStart, digitsEnd, true
		}
		from = start + 1
	}
	return 0, 0, 0, 0, false
}
