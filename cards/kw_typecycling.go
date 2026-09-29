// Keyword expansion split out of keywords.go so that tickets touching
// different keywords stop colliding on one file. Each expander is
// registered by head; a duplicate head panics (registerKeyword).

package cards

import "strings"

func kwTypeCycling(f *Face, i int, k, head, param string, has func(kind, line string) bool) {
	// CR 702.28d: typed cycling is an ordinary hand activation whose
	// resolution is a LIBRARY SEARCH, not a draw -- "[cost], Discard this
	// card: Search your library for a card with the [type] type, reveal
	// it, put it into your hand, then shuffle." The shape is therefore
	// Transmute's (search), never the plain Cycling case's AB$ Draw. The
	// reveal is the search's own default for a stated-quality
	// ChangeType$ (applyLibrarySearch's `spec != "Card"` arm), so no
	// Reveal$ is needed. param is "<type>:<cost>[...]"; the type is
	// fields[0] and the cost fields[1], with any trailing field a
	// human-readable description dropped -- the Landfall/etbCounter/
	// Equip trailing-field strip.
	if has("A", k) {
		return
	}
	typeSpec, rest, _ := strings.Cut(param, ":")
	typeSpec = strings.TrimSpace(typeSpec)
	cost, _, _ := strings.Cut(rest, ":")
	cost = strings.TrimSpace(cost)
	if sa := typeCyclingAbilitySA(typeSpec, cost); sa != nil {
		sa.Params["KeywordLine"] = k
		f.Abilities = append(f.Abilities, sa)
	}
}

// typeCyclingAbilitySA builds the typed-cycling ability body (CR 702.28d)
// the printed K:TypeCycling line expands to. It is shared by the printed
// expansion (kwTypeCycling) and the granted route
// (cards.GrantedKeywordAbility) so the two constructions cannot drift.
func typeCyclingAbilitySA(typeSpec, cost string) *SA {
	sa, _ := parseSA("", "AB$ ChangeZone | Cost$ "+cost+" Discard<1/CARDNAME> | ActivationZone$ Hand | Origin$ Library | Destination$ Hand | ChangeType$ "+typeSpec+" | ChangeNum$ 1 | Keyword$ TypeCycling | SpellDescription$ "+typeSpec+"cycling "+cost)
	return sa
}

func init() { registerKeyword(kwTypeCycling, "TypeCycling") }
