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
	for _, raw := range oracleLines(oracle) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "(") {
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
	return xmageCostText(selfRef(strings.TrimSpace(line[:i]), sourceName))
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
	var matches []string
	for _, raw := range oracleLines(f.Oracle) {
		line := stripReminder(strings.TrimSpace(raw))
		switch head {
		case "TypeCycling":
			// Forge's TypeCycling covers basic-landcycling and arbitrary
			// typecycling (for example Halflingcycling). Basic is the one
			// exception where the printed keyword includes "land".
			word := strings.TrimSpace(sa.ParamStr(cards.PKChangeType))
			if strings.EqualFold(word, "Basic") {
				word = "Basic land"
			}
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
			display := head
			// Job-named equipment prints "Job — Equip {N}". Match the
			// keyword after that printed header, while preserving the whole
			// XMage selector text.
			candidate := line
			if dash := strings.LastIndex(candidate, " — "); dash >= 0 {
				candidate = strings.TrimSpace(candidate[dash+len(" — "):])
			}
			if keywordLineStartsWith(candidate, display) {
				cost := strings.TrimSpace(sa.ParamStr(cards.PKCost))
				if cost != "" && candidate != display && hasPrintedManaCost(candidate, display, cost) {
					matches = append(matches, line)
				} else if cost == "" || candidate == display || !manaCostSymbols(cost) {
					// Non-mana / unparsed costs have no uniform brace spelling;
					// preserve the prior role-based match for those keywords.
					matches = append(matches, line)
				}
			}
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}

// hasPrintedManaCost checks the Oracle selector for Forge's space-separated
// mana-cost symbols in their printed, individually-braced form. This keeps a
// keyword name in ordinary prose (for example "Ninjutsu abilities...") from
// becoming a second candidate, while accepting multi-symbol costs such as
// Forge's "1 R" and Oracle's "{1}{R}".
func hasPrintedManaCost(line, display, cost string) bool {
	fields := strings.Fields(cost)
	var rendered strings.Builder
	for _, symbol := range fields {
		if !isManaCostSymbol(symbol) {
			break
		}
		rendered.WriteByte('{')
		rendered.WriteString(symbol)
		rendered.WriteByte('}')
	}
	return rendered.Len() > 0 && strings.Contains(line[len(display):], rendered.String())
}

// manaCostSymbols reports whether Forge's cost begins with mana-cost symbols.
// Some keyword costs append non-mana actions (Ninjutsu's return cost, for
// example), so only the leading symbols are used for printed-line matching.
func manaCostSymbols(cost string) bool {
	for _, field := range strings.Fields(cost) {
		if !isManaCostSymbol(field) {
			break
		}
		return true
	}
	return false
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
	case ' ', '{':
		return true
	}
	return false
}
