// XMage cost-text spelling for the level-B activate prefix (see
// xmage_ability.go). XMage selects an activated ability by
// ability.toString().startsWith(prefix), and its rule text spells several cost
// shapes differently from the Oracle line; xmageCostText rewrites the Oracle
// cost portion into XMage's spelling. Each rewrite cites the XMage source that
// renders it (Mage/src/main/java/mage/abilities/...):
//
//   - "Sacrifice this artifact" -> "Sacrifice {this}": SacrificeSourceCost
//     text is "sacrifice {this}"; ExileSourceCost likewise "exile {this}".
//   - "Exile {this} from your graveyard" -> "Exile this card from your
//     graveyard" (ExileSourceFromGraveCost), "Exile this card from your hand"
//     -> "Exile {this} from your hand" (ExileSourceFromHandCost) and
//     "Discard {this}" -> "Discard this card" (DiscardSourceCost).
//   - A leading "Word — " is "Exhaust &mdash; " for the keyword abilities
//     whose getRule() prepends the word (ExhaustAbility, PowerUpAbility,
//     BoastAbility, ForecastAbility) and "<i>Word</i> &mdash; " for an ability
//     or flavor word (AbilityImpl.getRule via CardUtil.italicizeWithEmDash).
//   - "Waterbend {N}" is the mana-cost text "waterbend {N}" (WaterbendCost,
//     WaterbendXCost); an ability-word header capitalises the rule's first
//     character, a keyword header does not.
package oraclegen

import (
	"regexp"
	"strings"
)

// keywordDashWords are the headers XMage prints as a bare "Word &mdash; "
// because the ability class's getRule() prepends them; every other header is
// an ability or flavor word, which XMage italicises.
var keywordDashWords = map[string]bool{
	"Exhaust":  true,
	"Power-up": true,
	"Boast":    true,
	"Forecast": true,
}

var (
	sacrificeThisRE   = regexp.MustCompile(`^Sacrifice this [A-Za-z]+$`)
	exileThisRE       = regexp.MustCompile(`^Exile this ([A-Za-z]+)$`)
	exileSelfGraveRE  = regexp.MustCompile(`^Exile (\{this\}|this card) from your graveyard$`)
	exileSelfHandRE   = regexp.MustCompile(`^Exile (\{this\}|this card) from your hand$`)
	discardSelfRE     = regexp.MustCompile(`^Discard (\{this\}|this card)$`)
	dashHeaderWordsRE = regexp.MustCompile(`^[^{},:]+$`)
)

// xmageCostText rewrites the cost portion of an ability line (the text before
// its first ": ", already {this}-rewritten by selfRef) into XMage's rule text.
// The short-name rewrite applies after the header split, so an ability word
// that names the card ("Calim's Breath — ") keeps its printed spelling.
func xmageCostText(cost, sourceName string) string {
	header, rest, keywordHeader := splitDashHeader(cost)
	rest = shortSelfRef(rest, sourceName)
	parts := strings.Split(rest, ", ")
	for i, part := range parts {
		parts[i] = xmageCostPart(part)
	}
	// Waterbend is a mana-cost symbol in XMage, so it prints lower-case at the
	// head of the rule unless an ability word's header capitalises it.
	if strings.HasPrefix(parts[0], "Waterbend ") && (header == "" || keywordHeader) {
		parts[0] = "waterbend " + strings.TrimPrefix(parts[0], "Waterbend ")
	}
	return header + strings.Join(parts, ", ")
}

// xmageCostPart rewrites one comma-separated cost part.
func xmageCostPart(part string) string {
	switch {
	case sacrificeThisRE.MatchString(part):
		return "Sacrifice {this}"
	case exileSelfGraveRE.MatchString(part):
		return "Exile this card from your graveyard"
	case exileSelfHandRE.MatchString(part):
		return "Exile {this} from your hand"
	case discardSelfRE.MatchString(part):
		return "Discard this card"
	}
	if m := exileThisRE.FindStringSubmatch(part); m != nil && m[1] != "card" {
		return "Exile {this}"
	}
	return part
}

// xmageRuleLine is selfRef followed by xmageCostText on the text before the
// line's first ": ".
func xmageRuleLine(line, sourceName string) string {
	line = selfRef(line, sourceName)
	i := strings.Index(line, ": ")
	if i < 0 {
		return line
	}
	return xmageCostText(strings.TrimSpace(line[:i]), sourceName) + line[i:]
}

// shortSelfRef rewrites a comma-named card's short name ("Ramos" for
// "Ramos, Dragon Engine") to {this} in the COST portion of a line only. The
// Oracle names the card by its short name where XMage's cost text has {this}:
// RemoveCountersSourceCost renders "remove five +1/+1 counters from {this}"
// and SacrificeSourceCost "sacrifice {this}", so "Remove five +1/+1 counters
// from Ramos" selects nothing in XMage. Rule text after the colon is left
// alone: an extension through it still fails closed (namesShortName), since
// the effect's own spelling is not derivable here.
func shortSelfRef(cost, sourceName string) string {
	short, _, ok := strings.Cut(sourceName, ",")
	short = strings.TrimSpace(short)
	if !ok || short == "" {
		return cost
	}
	var out strings.Builder
	for {
		i := strings.Index(cost, short)
		if i < 0 {
			out.WriteString(cost)
			return out.String()
		}
		end := i + len(short)
		// "Tocasia, Digsite Mentor" is the full name spelled differently
		// from Forge's, not a short-name reference; leave it alone.
		if wordByte(cost, i-1) || wordByte(cost, end) || strings.HasPrefix(cost[end:], ", ") {
			out.WriteString(cost[:end])
			cost = cost[end:]
			continue
		}
		out.WriteString(cost[:i] + "{this}")
		cost = cost[end:]
	}
}

// wordByte reports whether s[i] is a letter or digit (false out of range), so
// a short name is only rewritten as a whole word.
func wordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// splitDashHeader splits a leading "Word — " off cost and returns XMage's
// spelling of it. Forge's corpus prints one keyword header (Liliana the
// Repentant's "Exhaust - ") with an ASCII hyphen, accepted for the keyword
// words only.
func splitDashHeader(cost string) (header, rest string, keyword bool) {
	for _, sep := range []string{" \u2014 ", " - "} {
		i := strings.Index(cost, sep)
		if i <= 0 || !dashHeaderWordsRE.MatchString(cost[:i]) {
			continue
		}
		word := cost[:i]
		switch {
		case keywordDashWords[word]:
			return word + " &mdash; ", cost[i+len(sep):], true
		case sep == " \u2014 ":
			return "<i>" + word + "</i> &mdash; ", cost[i+len(sep):], false
		}
	}
	return "", cost, false
}
