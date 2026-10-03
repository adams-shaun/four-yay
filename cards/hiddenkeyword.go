// Forge spells some combat keywords as a full English sentence rather than a
// bare head ("CARDNAME must be blocked if able.", "CARDNAME can't attack or
// block alone."). A sentence line has no colon, so KeywordHead returns the
// WHOLE sentence and the coverage walk interns `kw:<sentence>` -- a phantom
// primitive that names no engine symbol, and a keyword no matcher recognises
// by its printed head. This file is the one place the sentence spellings are
// translated into the canonical heads the engine reads, so the next such
// sentence is one table entry rather than another special case at each
// consumer.
//
// The translation is applied in cards/parse.go as a printed K: line is read,
// NOT in rules: a sentence line is a PARSER artefact of Forge's script
// grammar, and every consumer of Face.Keywords (the primitive census, the
// ratchet, HasKeyword, rules' derived-keyword readers) must see the same
// canonical head. Runtime KW$ grants (a Pump/PumpAll's `KW$ HIDDEN CARDNAME
// must be blocked if able.`) never pass through the K: parser, so they keep
// the sentence form; rules reads BOTH spellings for the same meaning (see
// rules/combat/restrictions.go ParseHiddenKeyword), which is why the canonical-head arm
// there is additive, not a replacement.
package cards

import "strings"

// canonicalKeywordHeads maps a Forge sentence keyword head (case-insensitive,
// already trimmed) to the canonical head the engine reads by that name. Only
// heads whose meaning is ALREADY implemented are listed: a sentence whose
// behaviour the build does not model must stay an unknown primitive, because
// renaming it would advertise support that a matcher does not deliver.
//
//   - "CARDNAME must be blocked if able." (CR 509.1a, the attacker's
//     requirement to receive at least one legal blocker) is read as the
//     MustBlock keyword, the spelling rules/statics.go's
//     combat.HasMustBeBlockedKeyword accepts beside the Pump-granted sentence form.
//   - "CARDNAME can't attack or block." is read as CantAttackOrBlock, whose
//     two combat restrictions are both consumed by the rules readers.
var canonicalKeywordHeads = map[string]string{
	"cardname must be blocked if able.": "MustBlock",
	"cardname can't attack or block.":   "CantAttackOrBlock",
}

// HiddenUntapNextStepKeyword is the EXACT Forge sentence a Pump/PumpAll `KW$
// HIDDEN ...` grant uses for CR 611.2b's "doesn't untap during its
// controller's next untap step" one-shot (Frost Lynx, Kashi-Tribe Elite et
// al.). The phrase never appears as a printed `K:` line, so it is not in the
// canonicalKeywordHeads table: it exists only as runtime keyword TEXT, which
// is exactly why it must be read from the derived keyword list rather than by
// HasKeyword. The sentence is matched WHOLE -- Undiscovered Paradise's
// "During your next untap step, ..." rider shares a prefix and must not
// borrow this meaning.
const HiddenUntapNextStepKeyword = "This card doesn't untap during your next untap step."

// IsHiddenUntapNextStepKeyword reports whether one derived keyword line is
// Forge's next-untap-step restriction sentence, with or without the leading
// "HIDDEN " marker the corpus spells it both ways with. This is the ONE
// reader both the grant site (effects, which stamps the one-shot flag) and
// the untap step (rules, which consumes it) call, so a future equivalent
// spelling is one arm here rather than two that can drift apart.
func IsHiddenUntapNextStepKeyword(k string) bool {
	head := strings.TrimSpace(strings.TrimPrefix(KeywordHead(k), "HIDDEN "))
	return strings.EqualFold(head, HiddenUntapNextStepKeyword)
}

// CanonicalKeywordLine rewrites a keyword line whose head is a known Forge
// sentence spelling to its canonical head, preserving any parameter the line
// carries after its first colon. A line whose head is not in the table -- and
// a line already carrying the canonical head -- is returned unchanged, so the
// function is idempotent and cannot loop.
func CanonicalKeywordLine(k string) string {
	head := KeywordHead(k)
	canon, ok := canonicalKeywordHeads[strings.ToLower(head)]
	if !ok || canon == head {
		return k
	}
	if i := strings.IndexByte(k, ':'); i >= 0 {
		return canon + k[i:]
	}
	return canon
}
