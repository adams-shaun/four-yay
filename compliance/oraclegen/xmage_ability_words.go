// Per-card XMage ability words the Forge Oracle line does not print.
//
// XMage's AbilityImpl.addRulePrefix (Mage/src/main/java/mage/abilities/
// AbilityImpl.java:1526) prepends `abilityWord.formatWord()` =
// CardUtil.italicizeWithEmDash(word) = "<i>Word</i> &mdash; " when the card
// class sets one, so a selector derived from the bare Oracle line finds
// nothing. The corpus/IR exposes nothing here (no `AbilityWord$` exists in a
// Forge script), so the card classes that do it are mapped by hand; a card
// not in the map is unchanged and keeps its fail-closed behaviour. Known
// instances are only the two XMage classes of
// AddEachControlledColorManaAbility, and of those only Bloom Tender (ECL,
// one of the 20 Standard sets) sets a word; Faeburrow Elder sets none.
package oraclegen

import "strings"

// xmageAbilityWords maps a card name to the ability word XMage prepends to
// its activated ability's rule text when the Forge Oracle line prints none
// (BloomTender.java:26 `.setAbilityWord(AbilityWord.VIVID)`).
var xmageAbilityWords = map[string]string{
	"Bloom Tender": "Vivid",
}

// xmageAbilityWordHeader returns the header XMage prints in front of a
// mapped card's ability line, or "" when the card is not mapped or its line
// already carries a dash header (a Forge-printed word must not be doubled).
func xmageAbilityWordHeader(name, line string) string {
	word, ok := xmageAbilityWords[name]
	if !ok || lineDashHeader(line) != "" {
		return ""
	}
	return "<i>" + word + "</i> &mdash; "
}

// lineDashHeader renders the dash header the line's own cost portion already
// carries ("Solved &mdash; ", "<i>Grandeur</i> &mdash; "), using the same
// splitter the cost text uses; empty means none.
func lineDashHeader(line string) string {
	cost := line
	if i := strings.Index(line, ": "); i >= 0 {
		cost = line[:i]
	}
	header, _, _ := splitDashHeader(cost)
	return header
}
