package cards

import "strings"

// counterKeywordCounters is CR 122.1b's fixed list of keyword counters: a
// marker counter whose kind names one of these grants that keyword to the
// permanent it sits on. It mirrors Forge's CounterKeywordType.keywordCounter
// list verbatim (forge-game CounterKeywordType.java), which is the authority
// the card scripts are written against -- a script says `CounterType$ Menace`
// and expects CR 702.109's menace, not an inert marker.
//
// CR 122.1b names these without a zone restriction, and Forge's generated
// static carries EffectZone$ All, so the grant is live in every zone.
var counterKeywordCounters = [...]string{
	"Flying", "First Strike", "Double Strike", "Deathtouch", "Decayed",
	"Exalted", "Haste", "Hexproof", "Indestructible", "Lifelink", "Menace",
	"Reach", "Shadow", "Trample", "Vigilance",
}

// utf8RuneSelf is utf8.RuneSelf: bytes below it are single-byte ASCII.
const utf8RuneSelf = 0x80

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// counterKeywordFirst has bit (c & 63) set for the lower-cased first letter
// c of every counterKeywordCounters name (all ASCII letters, so the 6-bit
// fold is collision-free among them: 'a'..'z' are 0x61..0x7a).
var counterKeywordFirst = func() uint64 {
	var m uint64
	for _, name := range counterKeywordCounters {
		m |= 1 << (lowerASCII(name[0]) & 63)
	}
	return m
}()

// CounterKeyword maps a marker counter kind to the keyword it grants, matching
// case-insensitively and returning the CANONICAL keyword spelling (the same
// title-cased name Forge's CounterKeywordType.toString emits), plus whether
// the kind grants a keyword at all.
//
// A parameterised kind keeps its parameter: Forge's isKeywordCounter also
// admits `Hexproof:` and `Trample:` prefixes, and the parameter is part of the
// keyword line ("Hexproof:Black"), so `CounterType$ Hexproof:Black` grants
// Hexproof from black. The head before the colon is matched against the fixed
// list so the parameter rides through unchanged.
//
// A kind outside the list (P1P1, CHARGE, ENERGY, LOYALTY, ...) grants nothing
// and returns ("", false), so this is the ONE classifier every counter-to-
// keyword read goes through -- a future added keyword cannot be honoured by
// one consumer and ignored by another.
func CounterKeyword(kind string) (string, bool) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", false
	}
	// Every listed name starts with an ASCII letter, and strings.EqualFold
	// can match an ASCII letter only with itself in either case or with a
	// non-ASCII fold partner (U+017F for s, U+212A for k). A kind whose
	// first byte is ASCII and folds to no listed name's first letter cannot
	// match any of them: the common counters (P1P1, CHARGE, ...) stop here.
	if c := kind[0]; c < utf8RuneSelf && counterKeywordFirst&(1<<(lowerASCII(c)&63)) == 0 {
		return "", false
	}
	head := kind
	param := ""
	if i := strings.IndexByte(kind, ':'); i >= 0 {
		head = strings.TrimSpace(kind[:i])
		param = kind[i:]
	}
	for _, name := range counterKeywordCounters {
		if strings.EqualFold(name, head) {
			return name + param, true
		}
	}
	return "", false
}
