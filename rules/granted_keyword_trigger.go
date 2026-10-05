package rules

// The granted-keyword triggered-ability synthesizer, the triggered sibling of
// cards.GrantedKeywordAbility. A layer-6 AddKeyword$ grant lands its keyword in
// the recipient's DERIVED keyword list (CR 613.1f), but keyword expansion
// (cards/keywords.go expandKeywords) only reads a face's PRINTED keyword list,
// so no trigger is ever minted for the grant. This file is the one dispatcher
// the granted-trigger walk calls: it synthesizes the triggered ability for the
// heads whose body is a fixed, self-contained rule from the keyword LINE alone.
//
// It lives in rules rather than cards on purpose: cards/CompilerFingerprint
// hashes every cards/*.go source file, so adding this there would invalidate
// the compiled corpus cache on every machine. The synthesizer needs no
// compiler state beyond cards' exported types, so rules is its home.

import "github.com/adams-shaun/gorge/cards"

// grantedTriggerHeads is the set of keyword heads grantedKeywordTrigger can
// synthesize a TRIGGERED ability for. Every other head returns nil (fail
// closed): the expansion of a keyword like Afflict, Dethrone or Firebending
// mints a trigger whose body reads keyword-specific state (a defending player,
// an attack's attacker set, an N in the keyword text) through a dedicated walk
// (checkGrantedAfflictTriggers and kin), and handing back a wrong body would be
// worse than handing back none. Dispatch is by interned head ID, not a
// string case: the codeshape stringCaseLiterals ratchet is shrink-only, so a
// new `case "Literal":` arm is not allowed in rules/ (cards owns one such
// whitelist; this is the rules-side twin).
func grantedTriggerHeads(head string) bool {
	return cards.KeywordHeadIDOf(head) == cards.KeywordHeadIDOf("Prowess")
}

// grantedKeywordTrigger synthesizes the triggered ability a keyword LINE
// grants. The line is the full "Head:param..." text exactly as it sits in a
// layer-6 AddKeyword$ grant's derived entry ("Prowess"). The trigger reuses
// the printed expansion's exact shape (cards/kw_prowess.go: Mode$ SpellCast |
// ValidCard$ Card.nonCreature | ValidActivatingPlayer$ You, body
// DB$ Pump | Defined$ Self | NumAtt$ +1 | NumDef$ +1) so the granted body is
// byte-identical to the printed one's -- a granted Prowess and a printed
// K:Prowess are the same rule. The body is carried inline (Effect) rather than
// as an Execute$ SVar name: a granted keyword lives in the layer system, never
// on a face, so there is no table to resolve a name against. A line whose head
// mints no triggered ability returns nil (the totality stance every
// synthesizer takes).
func grantedKeywordTrigger(line string) *cards.Trigger {
	if !grantedTriggerHeads(cards.KeywordHead(line)) {
		return nil
	}
	return &cards.Trigger{Mode: "SpellCast", Params: map[string]string{
		"Mode": "SpellCast", "ValidCard": "Card.nonCreature",
		"ValidActivatingPlayer": "You", "TriggerDescription": "Prowess",
	}, Effect: &cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{
		"Defined": "Self", "NumAtt": "+1", "NumDef": "+1",
	}}}
}
