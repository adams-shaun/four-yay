package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The scoped combat-legality statics: a CantBlockBy / MinMaxBlocker /
// CantAttack whose restriction is scoped to somebody OTHER than the static's
// own host -- the creature an Equipment/Aura is attached to (the bearer), or
// every creature of a filter ("Creatures you control with power 2 or less
// can't be blocked by creatures with power 3 or greater"). The gated file
// (legality_gated.go) serves the self-scoped shapes; these are the sibling
// scopes the same observation reaches by fielding a matching and a
// non-matching creature beside the card.
//
// This file classifies only. Every parameter of a recognized line is one the
// template reads (the same strictness as gateParamsOnly): an unmodelled
// parameter, qualifier or mode stays the visible "legality static" gap. A
// recognized sub whose board the template cannot field is a named template
// skip, never this gap.

// scopedFilterTokens are the lower-cased "+" qualifiers of a creature filter
// the template's probe search reads: the controller words, the evasion words
// the engine resolves from a creature's keywords, the counters word and the
// token word. A "power<op><int>" compare, a "non<Type>" negation and a
// capitalised type word (Spider, Boar, Detective) are read beside them.
var scopedFilterTokens = map[string]bool{
	"youctrl": true, "youdontctrl": true, "withflying": true, "withdefender": true,
	"hascounters": true, "token": true, "enchantedby": true,
}

// scopedTypeWords are the type and subtype words of the template's probe
// pool (templates/static_legality_scoped.go): a filter naming any other
// creature type has no creature to field, so it stays the gap.
var scopedTypeWords = map[string]bool{
	"creature": true, "card": true, "permanent": true,
	"spider": true, "boar": true, "detective": true, "human": true, "cat": true,
	"bear": true, "giant": true, "elf": true, "wall": true, "wurm": true, "drake": true,
}

// isTypeWord reports whether tok is a capitalised alphabetic word naming one
// of scopedTypeWords, Forge's spelling of a card type or subtype qualifier.
func isTypeWord(tok string) bool {
	if tok == "" || tok[0] < 'A' || tok[0] > 'Z' {
		return false
	}
	return scopedTypeWords[strings.ToLower(tok)]
}

// negatedTypeWord reports whether tok is "non<Type>" (nonDetective).
func negatedTypeWord(tok string) bool {
	if len(tok) <= 3 || !strings.EqualFold(tok[:3], "non") {
		return false
	}
	return isTypeWord(strings.ToUpper(tok[3:4]) + tok[4:])
}

// scopedBlockerList is scopedFilterList for a ValidBlocker$ value: the
// template fields the blockers on the seat the attackers' opponent controls
// (or the Aura bearer's), so a YouCtrl alternative -- the static controller's
// own creatures blocking -- is not a board it can build.
func scopedBlockerList(filter string) bool {
	if !scopedFilterList(filter, false) {
		return false
	}
	for _, alt := range strings.Split(filter, ",") {
		_, rest, _ := strings.Cut(alt, ".")
		for _, tok := range strings.Split(rest, "+") {
			if strings.EqualFold(strings.TrimSpace(tok), "YouCtrl") {
				return false
			}
		}
	}
	return true
}

// scopedFilterOK reports whether filter is a "<Base>[.q+q...]" creature
// filter whose every qualifier the probe search reads. allowSelf admits the
// Self qualifier (an attacker filter naming the card itself).
func scopedFilterOK(filter string, allowSelf bool) bool {
	filter = strings.TrimSpace(filter)
	base, rest, hasQuals := strings.Cut(filter, ".")
	if !isTypeWord(base) {
		return false
	}
	if !hasQuals {
		return true
	}
	if rest == "" {
		return false
	}
	for _, tok := range strings.Split(rest, "+") {
		tok = strings.TrimSpace(tok)
		low := strings.ToLower(tok)
		switch {
		case scopedFilterTokens[low]:
		case low == "self" && allowSelf:
		case negatedTypeWord(tok):
		case isTypeWord(tok):
		default:
			if _, _, ok := PowerFilterBound(tok); !ok {
				return false
			}
		}
	}
	return true
}

// scopedFilterList reports whether every comma alternative of filter is a
// scopedFilterOK filter.
func scopedFilterList(filter string, allowSelf bool) bool {
	for _, alt := range strings.Split(filter, ",") {
		if !scopedFilterOK(alt, allowSelf) {
			return false
		}
	}
	return true
}

// scopedParamsOnly reports whether st carries only the Mode and display
// parameters plus the listed keys (each of which the caller reads).
func scopedParamsOnly(st *cards.Static, keys ...cards.ParamKey) bool {
	for k := range st.Params {
		if isDisplayKey(k, true) {
			continue
		}
		read := false
		for _, want := range keys {
			read = read || k == want.String()
		}
		if !read {
			return false
		}
	}
	return true
}

// bearerScope reports whether a ValidCard$/ValidAttacker$ value names the
// creature an Equipment or Aura is attached to and nothing narrower.
func bearerScope(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "creature.equippedby", "creature.enchantedby":
		return true
	}
	return false
}

// scopedLegalityStatic names the sub-family serving st's bearer- or
// filter-scoped combat-legality shape, or ok=false.
func scopedLegalityStatic(f *cards.Face, st *cards.Static) (string, bool) {
	switch strings.ToLower(st.Mode) {
	case "cantblockby":
		return scopedCantBlockBy(f, st)
	case "minmaxblocker":
		if !scopedParamsOnly(st, cards.PKValidCard, cards.PKMax) || MaxBlockerCap(st) == 0 {
			return "", false
		}
		vc := st.ParamStr(cards.PKValidCard)
		switch {
		case bearerScope(vc):
			return "static.max-blockers-bearer", true
		case scopedFilterOK(vc, false):
			return "static.max-blockers-filter", true
		}
	case "cantattack":
		if !scopedParamsOnly(st, cards.PKValidCard, cards.PKTarget) {
			return "", false
		}
		vc, target := st.ParamStr(cards.PKValidCard), st.ParamStr(cards.PKTarget)
		youTarget := strings.EqualFold(target, "You") || strings.EqualFold(target, "You,Planeswalker.YouCtrl")
		switch {
		case strings.EqualFold(vc, "Creature.EnchantedBy Aura.YouCtrl") && youTarget:
			return "static.cant-attack-bearer", true
		case youTarget && scopedFilterOK(vc, false) && !strings.EqualFold(vc, "Creature"):
			return "static.cant-attack-filter", true
		case strings.EqualFold(vc, "Creature") && strings.EqualFold(target, "Card.Self+AttachedTo Creature") && !f.IsCreature():
			return "static.cant-be-attacked-attached", true
		}
	}
	return "", false
}

// scopedCantBlockBy classifies a CantBlockBy: the bearer-unblockable
// Equipment, the filter pair (an attacker filter and/or a blocker filter),
// and the two self-scoped statics whose host is not a creature all the time
// (an animated land, a crewed Vehicle).
func scopedCantBlockBy(f *cards.Face, st *cards.Static) (string, bool) {
	va, vb := st.ParamStr(cards.PKValidAttacker), st.ParamStr(cards.PKValidBlocker)
	if scopedParamsOnly(st, cards.PKValidAttacker) && !st.HasParam(cards.PKValidBlocker) {
		switch {
		case bearerScope(va):
			return "static.cant-block-by-bearer", true
		case strings.EqualFold(va, "Creature.Self") && f.IsLand():
			return "static.cant-block-by-animated-self", true
		}
	}
	if scopedParamsOnly(st, cards.PKValidAttacker, cards.PKValidBlocker) && vb != "" &&
		scopedFilterOK(va, true) && scopedBlockerList(vb) {
		return "static.cant-block-by-filter", true
	}
	// A Vehicle's own gated "can't be blocked" (Watertight Gondola's Descend
	// 8): the host is a creature only while crewed, which gatedLegalityStatic
	// (a creature host) does not admit.
	if !f.IsCreature() && faceHasSubtype(f, "Vehicle") &&
		selfFilterTokensOK(va) && !st.HasParam(cards.PKValidBlocker) &&
		gateParamsOnly(st, cards.PKValidAttacker, true) {
		return "static.cant-block-by-crewed", true
	}
	return "", false
}
