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

// keywordDisplay names the printed words a keyword-expanded AB's line starts
// with when they differ from the keyword's IR head. Every other head prints
// as itself (Equip -> "Equip", Crew -> "Crew").
var keywordDisplay = map[string]string{
	"TypeCycling": "Basic landcycling",
}

// XMageAbility maps every activated ability of f (by index into
// f.Abilities) to the XMage rule-text prefix that selects it, or returns a
// non-empty reason the whole face's mapping is ambiguous. A caller that only
// needs one ability still fails closed for the face, because the ordinal
// mapping is derived from the whole face.
func XMageAbility(f *cards.Face) (map[int]string, string) {
	lines := abilityLines(f.Oracle)
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
	needsUnique := !intrinsicHasLine
	// full and base live in the same {this}-rewritten text space, so a shared
	// self-referential cost ("{T}, Sacrifice {this}") is seen as shared and an
	// extension into the rule text never copies the printed name.
	full := make([]string, len(nonKeyword))
	base := make([]string, len(nonKeyword))
	for k, i := range nonKeyword {
		full[k] = selfRef(lines[k], f.Name)
		base[k] = linePrefix(lines[k], f.Name)
		if loyalty := loyaltyCost(f.Abilities[i].ParamStr(cards.PKCost)); loyalty != "" {
			needsUnique = true
			colon := strings.Index(full[k], ": ")
			if colon < 0 {
				return nil, "activate xmage text ambiguous"
			}
			full[k] = loyalty + ": " + strings.TrimSpace(full[k][colon+2:])
			base[k] = loyalty
		}
	}
	for k := range base {
		needsUnique = needsUnique || sharedBase(base, k)
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
			text, ok = keywordPrefix(f, keyword)
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
		prefix := base[k]
		if needsUnique {
			prefix = extendPrefix(prefix, full, k)
			if conflictsWithOtherLine(prefix, k, full) || namesShortName(prefix[len(base[k]):], f.Name) {
				return nil, "activate xmage text ambiguous"
			}
		}
		if prefix == "" || seen[prefix] {
			return nil, "activate xmage text ambiguous"
		}
		seen[prefix] = true
		out[i] = prefix
	}
	// A unique full-line match must also be unique against every emitted
	// prefix; fail closed if a prefix ever leaves its own line's text space.
	if needsUnique {
		prefixes := make([]string, len(nonKeyword))
		for k, i := range nonKeyword {
			prefixes[k] = out[i]
		}
		for k, prefix := range prefixes {
			if !strings.HasPrefix(full[k], prefix) || conflictsWithOtherLine(prefix, k, prefixes) {
				return nil, "activate xmage text ambiguous"
			}
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

// sharedBase identifies faces that the legacy mapper skipped for duplicate
// costs. Together with loyalty and line-less intrinsics, these faces need a
// startsWith-unique mapping. Other faces preserve their legacy prefixes.
func sharedBase(base []string, own int) bool {
	for i, b := range base {
		if i != own && b == base[own] {
			return true
		}
	}
	return false
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
// XMage matches on, with the card's name rewritten by selfRef.
func linePrefix(line, sourceName string) string {
	i := strings.Index(line, ": ")
	if i < 0 {
		return ""
	}
	return selfRef(strings.TrimSpace(line[:i]), sourceName)
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

// keywordPrefix finds the keyword-expanded AB's printed line and returns its
// reminder-stripped text. display is the keyword's printed name (Equip,
// Cycling, Basic landcycling for TypeCycling). The line must start with that
// name followed by a space, a brace or the end of line, so "Crew" does not
// match "Crewmate".
func keywordPrefix(f *cards.Face, head string) (string, bool) {
	display := head
	if alias, ok := keywordDisplay[head]; ok {
		display = alias
	}
	for _, raw := range oracleLines(f.Oracle) {
		line := stripReminder(strings.TrimSpace(raw))
		if !keywordLineStartsWith(line, display) {
			continue
		}
		return line, true
	}
	return "", false
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
