// XMage ability-text derivation (hypothesis H1,
// docs/superpowers/specs/2026-10-05-compliance-level-b.md section 2.1).
//
// XMage's TestPlayer selects an activated ability by a prefix of its printed
// rule text -- activateAbility(turn, step, player, "{2}, {T}") -- so a level-B
// activate scenario must carry that prefix outside the scenario body (which
// rules' runner decodes strictly). The mapping is kept in its own file so the
// L6 driver ticket can adjust it without touching the template:
//
//   - The k-th oracle line that is an ACTIVATED-ABILITY line maps to the k-th
//     non-keyword AB$ in IR order, and that line's text before its first ": "
//     is the prefix.
//   - A keyword-expanded AB (Equip, Cycling, Station, ...) maps to its
//     keyword line's text ("Equip {2}"), found by the keyword's printed name.
//   - A loyalty cost prints as XMage does ("+1", "-3", "0", "-X"), without
//     the Oracle brackets.
//   - Loyalty, shared-cost and line-less intrinsic mappings must select
//     uniquely under startsWith: conflicting prefixes extend a word at a
//     time in {this}-rewritten text ("+1: Exile" vs "+1: Add", "-1:").
//     Other faces retain their legacy cost-only mappings.
//   - An injected basic-land-type mana ability with no printed line maps to
//     XMage's "{T}: Add {C}." text.
//   - When the two ordinal counts differ, a keyword line cannot be found, or
//     two abilities still share a prefix, the mapping is ambiguous and the
//     caller skips.
package oraclegen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// XMageAbility maps every activated ability of f (by index into
// f.Abilities) to the XMage rule-text prefix that selects it, or returns a
// non-empty reason the whole face's mapping is ambiguous. A caller that only
// needs one ability still fails closed for the face, because the ordinal
// mapping is derived from the whole face.
func XMageAbility(f *cards.Face) (map[int]string, string) {
	lines := abilityLines(f.Oracle)
	// Keyword-expanded activations can also have colon-bearing printed
	// selector lines (Class: "{cost}: Level N"). They belong to that
	// keyword AB, not to the ordinal list of ordinary activated abilities.
	lines = removeKeywordAbilityLines(f, lines)
	// An injected basic-land-type mana ability usually has no printed line (a
	// dual land's second type), but a face whose Oracle prints "{T}: Add {B}."
	// (the Gates) has one. Keep the intrinsic in the ordinal match when that is
	// what makes the counts agree; drop it only when dropping it does.
	nonKeyword := nonKeywordAbilities(f, true)
	intrinsicHasLine := len(lines) == len(nonKeyword)
	if !intrinsicHasLine {
		nonKeyword = nonKeywordAbilities(f, false)
	}
	if len(lines) != len(nonKeyword) {
		return nil, "activate xmage text ambiguous"
	}
	out := make(map[int]string, len(f.Abilities))
	seen := make(map[string]bool)
	// full and base live in the same {this}-rewritten text space, so a shared
	// self-referential cost ("{T}, Sacrifice {this}") is seen as shared and an
	// extension into the rule text never copies the printed name.
	full := make([]string, len(nonKeyword))
	base := make([]string, len(nonKeyword))
	for k, i := range nonKeyword {
		full[k] = xmageRuleLine(lines[k], f.Name)
		base[k] = linePrefix(lines[k], f.Name)
		if loyalty := loyaltyCost(f.Abilities[i].ParamStr(cards.PKCost)); loyalty != "" {
			colon := strings.Index(full[k], ": ")
			if colon < 0 {
				return nil, "activate xmage text ambiguous"
			}
			full[k] = loyalty + ": " + strings.TrimSpace(full[k][colon+2:])
			base[k] = loyalty
		}
	}
	// Ordinal matching excludes line-less intrinsics, but startsWith selection
	// must include them. Assemble EVERY selectable line before extending any
	// prefix, including keyword abilities, so insertion order cannot hide a
	// conflict (Murmuring Bosk's printed {T} versus its Forest intrinsic).
	for i, sa := range f.Abilities {
		if !sa.IsActivated() {
			continue
		}
		var text string
		if !intrinsicHasLine && intrinsicLandMana(sa) {
			text = intrinsicManaText(sa)
		} else if keyword := sa.ParamStr(cards.PKKeyword); keyword != "" {
			var ok bool
			text, ok = keywordPrefix(f, sa)
			if !ok {
				return nil, "activate xmage text ambiguous"
			}
		} else {
			continue
		}
		nonKeyword = append(nonKeyword, i)
		base = append(base, text)
		full = append(full, text)
	}
	for k, i := range nonKeyword {
		prefix := extendPrefix(base[k], full, k)
		if conflictsWithOtherLine(prefix, k, full) || namesShortName(prefix[len(base[k]):], f.Name) {
			return nil, "activate xmage text ambiguous"
		}
		if prefix == "" || seen[prefix] {
			return nil, "activate xmage text ambiguous"
		}
		seen[prefix] = true
		out[i] = prefix
	}
	// A unique full-line match must also be unique against every emitted
	// prefix; fail closed if a prefix ever leaves its own line's text space.
	prefixes := make([]string, len(nonKeyword))
	for k, i := range nonKeyword {
		prefixes[k] = out[i]
	}
	for k, prefix := range prefixes {
		if !strings.HasPrefix(full[k], prefix) || conflictsWithOtherLine(prefix, k, prefixes) {
			return nil, "activate xmage text ambiguous"
		}
	}
	return out, ""
}

// loyaltyCost renders Forge's bracketed loyalty counter cost as XMage's
// printed cost. Empty means this is not a loyalty cost.
func loyaltyCost(cost string) string {
	for _, part := range strings.Split(cost, ",") {
		part = strings.TrimSpace(part)
		for _, spec := range []struct{ prefix, sign string }{{"AddCounter<", "+"}, {"SubCounter<", "-"}} {
			if !strings.HasPrefix(part, spec.prefix) || !strings.HasSuffix(part, "/LOYALTY>") {
				continue
			}
			n := strings.TrimSuffix(strings.TrimPrefix(part, spec.prefix), "/LOYALTY>")
			if n == "X" {
				return "-X"
			}
			if n == "0" {
				return "0"
			}
			if spec.sign == "-" {
				return "-" + n
			}
			return "+" + n
		}
	}
	return ""
}

// extendPrefix lengthens prefix one word at a time into its own full line
// until no other line starts with it, the longer prefix XMage's startsWith
// selection needs to tell abilities apart, including -1 versus -10 costs.
func extendPrefix(prefix string, full []string, own int) string {
	line := full[own]
	for len(prefix) < len(line) && conflictsWithOtherLine(prefix, own, full) {
		end := len(prefix)
		for end < len(line) && line[end] == ' ' {
			end++
		}
		for end < len(line) && line[end] != ' ' {
			end++
		}
		prefix = line[:end]
	}
	return prefix
}

// conflictsWithOtherLine reports whether prefix would select another ability
// line under XMage's startsWith matching rule.
func conflictsWithOtherLine(prefix string, own int, lines []string) bool {
	for i, line := range lines {
		if i != own && strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

// namesShortName reports whether text mentions the short name of a comma-
// named card ("Chandra" for "Chandra, Torch of Defiance"). The Oracle prints
// it where XMage's rule text has {this}, so an extension through it could not
// match and the mapping fails closed.
func namesShortName(text, name string) bool {
	short, _, ok := strings.Cut(name, ",")
	return ok && short != "" && strings.Contains(strings.ToLower(text), strings.ToLower(short))
}

// nonKeywordAbilities lists the indices of f's activated ABs that are not
// keyword-expanded, in IR order; withIntrinsic keeps the injected basic-land
// mana abilities.
func nonKeywordAbilities(f *cards.Face, withIntrinsic bool) []int {
	var out []int
	for i, sa := range f.Abilities {
		if sa.IsActivated() && sa.ParamStr(cards.PKKeyword) == "" && (withIntrinsic || !intrinsicLandMana(sa)) {
			out = append(out, i)
		}
	}
	return out
}

// intrinsicLandMana identifies the mana ability cards.ApplyIntrinsics injects
// for a basic land subtype (CR 305.6), by the marker the injector stamps on
// it, so a land's own printed "{T}: Add {C}." ability is never mistaken for one.
func intrinsicLandMana(sa *cards.SA) bool {
	return sa.Line == intrinsicManaLine
}

// intrinsicManaLine is the SA.Line cards.IntrinsicManaAbility stamps on the
// ability it builds.
const intrinsicManaLine = "intrinsic: basic land mana"

func intrinsicManaText(sa *cards.SA) string {
	return fmt.Sprintf("{T}: Add {%s}.", strings.TrimSpace(sa.ParamStr(cards.PKProduced)))
}

// oracleLines splits a face's Oracle text into its printed lines. The corpus
// stores the line separators as the two characters "\n" (a backslash and an
// n), not a real newline; a card built directly by a test may carry either,
// so a real newline is normalised to the corpus spelling before splitting.
func oracleLines(oracle string) []string {
	return strings.Split(strings.ReplaceAll(oracle, "\n", `\n`), `\n`)
}

// abilityLines lists the oracle lines that are activated-ability lines, in
// order: a line whose reminder text is stripped still contains ": " and does
// not start with "(". Stripping the trailing parenthetical reminder is what
// keeps a keyword line ("Cycling {2} ({2}, Discard this card: Draw a card.)")
// out of the list; the whole-line parenthetical a conditional mana ability is
// granted under ("({T}: Add {G} or {U}.)") is excluded by the leading "(".
func abilityLines(oracle string) []string {
	var out []string
	gated := false
	for _, raw := range oracleLines(oracle) {
		line := spaceAfterCostColon(strings.TrimSpace(raw))
		if line == "" || strings.HasPrefix(line, "(") {
			continue
		}
		// Forge models the abilities printed under a "LEVEL N-M" / "STATION N+"
		// header (the rest of the face) as SVar grants, and "Max speed —"
		// lines as nothing at all, so neither has an AB to pair with.
		gated = gated || levelHeaderRE.MatchString(line)
		if gated || strings.HasPrefix(line, "Max speed — ") {
			continue
		}
		colon := strings.Index(line, ": ")
		if colon < 0 || !strings.Contains(stripReminder(line), ": ") {
			continue
		}
		// A quoted granted ability ("... and has \"{T}, Sacrifice this
		// creature: ...\"") carries its own ": " inside the quotes; it is
		// reminder text for a different object, not this face's ability line.
		if q := strings.IndexByte(line, '"'); q >= 0 && q < colon {
			continue
		}
		out = append(out, line)
	}
	return out
}

// levelHeaderRE matches the "LEVEL 1-2" / "LEVEL 8+" / "STATION 12+" line
// that opens a leveler's or Spacecraft's granted-ability sections.
var levelHeaderRE = regexp.MustCompile(`^(LEVEL|STATION) \d+(-\d+|\+)$`)

// costColonRE finds the cost/effect colon of a line printed without the
// space after it ("{T}:Draw a card.", "[-3]:You draw"), which a braced cost or
// a bracketed loyalty cost always precedes.
var costColonRE = regexp.MustCompile(`([}\]]):([^ ])`)

// spaceAfterCostColon restores the space after the cost colon, only on the
// first such colon so a colon inside the effect text is left alone.
func spaceAfterCostColon(line string) string {
	loc := costColonRE.FindStringSubmatchIndex(line)
	if loc == nil || strings.Contains(line[:loc[0]], ": ") {
		return line
	}
	return line[:loc[1]-1] + " " + line[loc[1]-1:]
}

// stripReminder removes a line's trailing parenthetical reminder text, the
// "(...)" that follows the printed rules text of a keyword or ability line.
// Only a trailing " (...)" is removed, so a line whose own text carries a
// parenthesis is untouched.
func stripReminder(line string) string {
	for {
		i := strings.LastIndex(line, " (")
		if i < 0 || !strings.HasSuffix(line, ")") {
			return strings.TrimSpace(line)
		}
		line = strings.TrimSpace(line[:i])
	}
}

// linePrefix returns the text before the line's first ": ", the cost prefix
// XMage matches on, with the card's name rewritten by selfRef and the cost spelled as XMage
// renders it (xmageCostText).
func linePrefix(line, sourceName string) string {
	i := strings.Index(line, ": ")
	if i < 0 {
		return ""
	}
	return xmageCostText(selfRef(strings.TrimSpace(line[:i]), sourceName), sourceName)
}

// selfRef rewrites every mention of the card's name in text to {this}:
// XMage's no-argument AbilityImpl.getRule() leaves the source placeholder
// literal, so a self-referential cost or effect is rendered with {this}, not
// the card's printed name.
func selfRef(text, sourceName string) string {
	if sourceName == "" {
		return text
	}
	lowerText, lowerName := strings.ToLower(text), strings.ToLower(sourceName)
	for from := 0; ; {
		rel := strings.Index(lowerText[from:], lowerName)
		if rel < 0 {
			break
		}
		start := from + rel
		text = text[:start] + "{this}" + text[start+len(sourceName):]
		lowerText = lowerText[:start] + "{this}" + lowerText[start+len(sourceName):]
		from = start + len("{this}")
	}
	return text
}

func removeKeywordAbilityLines(f *cards.Face, lines []string) []string {
	owned := make(map[string]bool)
	for _, sa := range f.Abilities {
		if !sa.IsActivated() || sa.ParamStr(cards.PKKeyword) == "" {
			continue
		}
		if line, ok := keywordPrefix(f, sa); ok {
			owned[line] = true
		}
	}
	out := lines[:0]
	for _, line := range lines {
		if !owned[strings.TrimSpace(line)] {
			out = append(out, line)
		}
	}
	return out
}

// keywordPrefix finds the keyword-expanded AB's printed line and returns its
// reminder-stripped text. For ordinary keyword abilities the line starts with
// the keyword's printed name, bounded so "Crew" does not match "Crewmate";
// Class, TypeCycling and job-named Equip use their distinct Oracle shapes.
func keywordPrefix(f *cards.Face, sa *cards.SA) (string, bool) {
	head := sa.ParamStr(cards.PKKeyword)
	var lines []string
	for _, raw := range oracleLines(f.Oracle) {
		lines = append(lines, stripReminder(strings.TrimSpace(raw)))
	}
	var matches []string
	if head == "TypeCycling" {
		// One printed line lists every landcycling ("Swampcycling {2},
		// mountaincycling {2}") and XMage prints each as its own ability.
		matches = keywordMatches(head, sa, commaSegments(lines))
	} else if matches = keywordMatches(head, sa, lines); len(matches) == 0 {
		// A comma-joined keyword list ("Madness {R}, cycling {1}{R}, ...").
		matches = keywordMatches(head, sa, commaSegments(lines))
	}
	// Prose that merely names the keyword ("Equip abilities you activate of
	// other Equipment cost {1} less...") also starts with it, and a face may
	// print the keyword twice ("Equip Detective {1}", "Equip {3}"). Only when
	// that leaves several candidates does the printed line decide: first the
	// line that spells this ability's leading cost symbol (or, for a cost with
	// none, the line whose cost is a dash-led "Equip—Sacrifice ..."), then the
	// line whose cost follows the keyword name directly. Forge's cost spelling
	// is never required to round-trip -- it collapses hybrid, Phyrexian, tap and
	// min-count symbols -- so a narrowing that finds nothing is skipped.
	if len(matches) > 1 {
		if symbol := leadingCostSymbol(sa.ParamStr(cards.PKCost)); symbol != "" {
			matches = narrowMatches(matches, func(line string) bool { return strings.Contains(line, symbol) })
		} else {
			matches = narrowMatches(matches, func(line string) bool { return dashLed(keywordCandidate(line), head) })
		}
	}
	if len(matches) > 1 {
		matches = narrowMatches(matches, func(line string) bool { return costLed(keywordCandidate(line), head) })
	}
	if len(matches) == 0 {
		return printedEquip(sa)
	}
	if len(matches) != 1 {
		return "", false
	}
	return xmageKeywordText(head, sa, matches[0])
}

// keywordMatches returns the lines that print this keyword ability.
func keywordMatches(head string, sa *cards.SA, lines []string) []string {
	var matches []string
	for _, line := range lines {
		switch head {
		case "TypeCycling":
			// Forge's TypeCycling covers basic-landcycling, arbitrary
			// typecycling (for example Halflingcycling) and dotted type
			// specs (Sojourner's Companion's Land.Artifact, printed
			// "Artifact landcycling").
			word := typeCyclingWord(sa.ParamStr(cards.PKChangeType))
			if word != "" && strings.HasPrefix(strings.ToLower(line), strings.ToLower(word+"cycling")) {
				matches = append(matches, line)
			}
		case "Class":
			// A Class level-up AB is printed as "{cost}: Level N", not as
			// a line beginning with the keyword name.
			if strings.HasSuffix(strings.ToLower(line), ": level "+sa.ParamStr(cards.PKLevel)) {
				matches = append(matches, line)
			}
		default:
			// Job-named equipment prints "Job — Equip {N}". Match the
			// keyword after that printed header, while preserving the whole
			// XMage selector text.
			if keywordLineStartsWith(keywordCandidate(line), head) {
				matches = append(matches, line)
			}
		}
	}
	return matches
}

// typeCyclingWord renders the printed word a TypeCycling AB's line begins
// with. "Basic" is printed "Basic land"; a dotted spec (Land.Artifact) is
// printed in English word order, the reverse of the dotted order.
func typeCyclingWord(spec string) string {
	spec = strings.TrimSpace(spec)
	if strings.EqualFold(spec, "Basic") {
		return "Basic land"
	}
	parts := strings.Split(spec, ".")
	if len(parts) < 2 {
		return spec
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.ToLower(strings.Join(parts, " "))
}

// commaSegments splits every line that lists several comma-separated
// abilities into those abilities, first letter upper-cased as XMage prints it.
func commaSegments(lines []string) []string {
	var out []string
	for _, line := range lines {
		for _, seg := range strings.Split(line, ", ") {
			if seg = strings.TrimSpace(seg); seg != "" {
				out = append(out, strings.ToUpper(seg[:1])+seg[1:])
			}
		}
	}
	return out
}

// xmageKeywordText spells the matched printed line as XMage renders the
// ability. A mana cost reads "Cycling {2}" in both; a non-mana cost is printed
// "Equip—Sacrifice a creature." by EquipAbility and CyclingAbility
// ("&mdash;"), and "Eternalize {2}{W}{W}, Discard a card" by EternalizeAbility.
// Other keywords' dash-led shapes have no rendering this mapping knows, and a
// mixed mana-and-other Equip/Cycling cost ("Equip—{2}, Pay 2 life.") is
// rendered in an order the printed line does not give, so those fail closed.
func xmageKeywordText(head string, sa *cards.SA, line string) (string, bool) {
	candidate := keywordCandidate(line)
	if !keywordLineStartsWith(candidate, head) {
		// Class and TypeCycling lines do not begin with the AB's keyword name.
		return line, true
	}
	header := flavorHeader(line[:len(line)-len(candidate)])
	name, rest := candidate[:len(head)], candidate[len(head):]
	switch {
	case strings.HasPrefix(rest, ":"):
		return header + name + " " + strings.TrimSpace(rest[1:]), true
	case !strings.HasPrefix(rest, "—") && !strings.HasPrefix(rest, "-"):
		return header + candidate, true
	}
	cost := strings.TrimLeft(rest, "—-")
	switch head {
	case "Equip", "Cycling":
		// A dash-led line whose cost text itself spells the ability's
		// leading mana symbol IS the ability's printed alternate-cost form
		// ("Equip—Pay {3} or discard a card.", "Equip—{2}, Pay 2 life.");
		// a dash-led line that does not spell it belongs to another
		// ability, and printedEquip supplies the plain "Equip {N}" text.
		if symbol := leadingCostSymbol(sa.ParamStr(cards.PKCost)); symbol != "" && !strings.Contains(cost, symbol) {
			return "", false
		}
		return header + name + "&mdash;" + cost, true
	case "Eternalize":
		return header + name + " " + strings.TrimSuffix(cost, "."), true
	}
	return "", false
}

// flavorHeader spells a printed "Job — " header as AbilityImpl.addRulePrefix
// renders a flavor word (CardUtil.italicizeWithEmDash).
func flavorHeader(header string) string {
	word := strings.TrimSuffix(header, " — ")
	if word == header {
		return header
	}
	return "<i>" + word + "</i> &mdash; "
}

// printedEquip spells an Equip ability whose Oracle text prints no Equip line
// at all (Forge omits it from some cards' Oracle: Buster Sword, Glamdring) as
// XMage renders a plain mana cost: "Equip {2}".
func printedEquip(sa *cards.SA) (string, bool) {
	if sa.ParamStr(cards.PKKeyword) != "Equip" {
		return "", false
	}
	fields := strings.Fields(sa.ParamStr(cards.PKCost))
	var text strings.Builder
	for _, field := range fields {
		if !isManaCostSymbol(field) {
			return "", false
		}
		text.WriteString("{" + field + "}")
	}
	if text.Len() == 0 {
		return "", false
	}
	return "Equip " + text.String(), true
}

// dashLed reports whether the keyword's cost is a dash-led non-mana cost
// ("Equip—Sacrifice a creature").
func dashLed(line, display string) bool {
	rest := strings.TrimSpace(line[len(display):])
	return strings.HasPrefix(rest, "—") || strings.HasPrefix(rest, "-")
}

// keywordCandidate strips a Job-style header ("Perseus's Bow — Equip {6}")
// from a printed line, leaving the text that begins with the keyword name.
func keywordCandidate(line string) string {
	if dash := strings.LastIndex(line, " — "); dash >= 0 {
		return strings.TrimSpace(line[dash+len(" — "):])
	}
	return line
}

// costLed reports whether the keyword's cost follows its printed name
// directly ("Equip {1}{R}", "Equip—Sacrifice a creature"), as opposed to prose.
func costLed(line, display string) bool {
	rest := strings.TrimSpace(line[len(display):])
	return rest == "" || strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "—") || strings.HasPrefix(rest, "-")
}

// leadingCostSymbol renders the first symbol of Forge's cost as a printed
// "{X}", or "" when it is not a plain symbol.
func leadingCostSymbol(cost string) string {
	fields := strings.Fields(cost)
	if len(fields) == 0 || !isManaCostSymbol(fields[0]) {
		return ""
	}
	return "{" + fields[0] + "}"
}

// narrowMatches keeps the lines keep accepts, or all of them when none does.
func narrowMatches(lines []string, keep func(string) bool) []string {
	var out []string
	for _, line := range lines {
		if keep(line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return lines
	}
	return out
}

func isManaCostSymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	for _, r := range symbol {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '/') {
			return false
		}
	}
	return true
}

// keywordLineStartsWith reports whether line begins with the printed keyword
// name display.
func keywordLineStartsWith(line, display string) bool {
	if len(line) < len(display) || !strings.EqualFold(line[:len(display)], display) {
		return false
	}
	if len(line) == len(display) {
		return true
	}
	switch line[len(display)] {
	case ' ', '{', ':':
		return true
	}
	// "Equip—Sacrifice a creature": the dash is a multi-byte rune.
	return dashLed(line, display)
}
