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
//   - When the two ordinal counts differ, a keyword line cannot be found, or
//     two abilities share a prefix, the mapping is ambiguous and the caller
//     skips.
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
	var nonKeyword []int
	for i, sa := range f.Abilities {
		if sa.IsActivated() && sa.ParamStr(cards.PKKeyword) == "" && !intrinsicLandMana(f, sa) {
			nonKeyword = append(nonKeyword, i)
		}
	}
	if len(lines) != len(nonKeyword) {
		return nil, "activate xmage text ambiguous"
	}
	out := make(map[int]string, len(f.Abilities))
	seen := make(map[string]bool)
	full := make([]string, len(nonKeyword))
	base := make([]string, len(nonKeyword))
	for k, i := range nonKeyword {
		full[k] = lines[k]
		base[k] = linePrefix(lines[k], f.Name)
		if loyalty := loyaltyCost(f.Abilities[i].ParamStr(cards.PKCost)); loyalty != "" {
			colon := strings.Index(lines[k], ": ")
			if colon < 0 {
				return nil, "activate xmage text ambiguous"
			}
			full[k] = loyalty + ": " + strings.TrimSpace(lines[k][colon+2:])
			base[k] = loyalty
		}
	}
	for k, i := range nonKeyword {
		prefix := base[k]
		for len(prefix) < len(full[k]) && conflictsWithOtherLine(prefix, k, full) {
			end := len(prefix)
			for end < len(full[k]) {
				end++
				if full[k][end-1] == ' ' {
					for end < len(full[k]) && full[k][end] != ' ' {
						end++
					}
					break
				}
			}
			prefix = full[k][:end]
		}
		if prefix == "" || conflictsWithOtherLine(prefix, k, full) {
			return nil, "activate xmage text ambiguous"
		}
		out[i] = prefix
		seen[prefix] = true
	}
	for i, sa := range f.Abilities {
		if sa.IsActivated() && intrinsicLandMana(f, sa) {
			out[i] = intrinsicManaText(sa)
		}
	}
	for i, sa := range f.Abilities {
		if !sa.IsActivated() || sa.ParamStr(cards.PKKeyword) == "" {
			continue
		}
		prefix, ok := keywordPrefix(f, sa.ParamStr(cards.PKKeyword))
		if !ok || seen[prefix] {
			return nil, "activate xmage text ambiguous"
		}
		seen[prefix] = true
		out[i] = prefix
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

// intrinsicLandMana identifies a mana ability generated from a land subtype,
// rather than a printed activated ability. Such an ability has no Oracle line.
func intrinsicLandMana(f *cards.Face, sa *cards.SA) bool {
	if !f.IsLand() || sa.API != "Mana" || strings.TrimSpace(sa.ParamStr(cards.PKCost)) != "T" {
		return false
	}
	produced := strings.TrimSpace(sa.ParamStr(cards.PKProduced))
	for _, typ := range f.Types {
		color := map[string]string{"Plains": "W", "Island": "U", "Swamp": "B", "Mountain": "R", "Forest": "G"}[typ]
		if color != "" && produced == color {
			return true
		}
	}
	return false
}

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
// XMage matches on. XMage's no-argument AbilityImpl.getRule() leaves the
// source placeholder literal, so a self-referential cost is rendered with
// {this}, not the card's printed name.
func linePrefix(line, sourceName string) string {
	i := strings.Index(line, ": ")
	if i < 0 {
		return ""
	}
	prefix := strings.TrimSpace(line[:i])
	if sourceName == "" {
		return prefix
	}
	lowerPrefix, lowerName := strings.ToLower(prefix), strings.ToLower(sourceName)
	for from := 0; ; {
		rel := strings.Index(lowerPrefix[from:], lowerName)
		if rel < 0 {
			break
		}
		start := from + rel
		prefix = prefix[:start] + "{this}" + prefix[start+len(sourceName):]
		lowerPrefix = lowerPrefix[:start] + "{this}" + lowerPrefix[start+len(sourceName):]
		from = start + len("{this}")
	}
	return prefix
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
