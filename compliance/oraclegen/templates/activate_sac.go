package templates

import (
	"strconv"
	"strings"
)

// This file is the single home of the Sac<N/Filter> cost vocabulary the
// activate template can satisfy. Both the fixture adder (which places the
// sacrificed permanent on p0's battlefield) and the XMage answer scripter
// (which names that permanent for XMage's sacrifice picker) read
// sacFilterFixture, so the permanent gorge sacrifices and the answer XMage is
// scripted with can never disagree. Before this the two call sites each
// carried their own substring chain, which is exactly the drift a shared
// table removes.

// sacSelf reports whether a Sac<...> token sacrifices the source itself:
// Sac<N/CARDNAME>, Sac<N/CARDNAME/this creature>, Sac<N/NICKNAME>. Forge's
// NICKNAME is the source's own name (the engine's cast_subcounter.go treats
// CARDNAME and NICKNAME alike), so both read as self-sacrifices with no
// fixture to place and no answer to script.
func sacSelf(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	fields := strings.Split(payload, "/")
	if len(fields) < 2 {
		return false
	}
	return strings.EqualFold(fields[1], "CARDNAME") || strings.EqualFold(fields[1], "NICKNAME")
}

// sacFixtureCards maps a Sac filter's base word (the text before the first
// '.') to the battlefield fixture that pays it. The base is deliberately
// coarse: Creature.Other, Creature.IsSuspected and a bare Creature all read
// "creature", and a qualifier that makes a plain fixture illegal (a token, an
// attachment, a suspected status) is rejected by sacFilterClause before the
// lookup. Every name is a card the corpus carries and XMage's card database
// carries; the host pass confirms the XMage side (spec hypothesis H4).
var sacFixtureCards = map[string]string{
	"creature":     "Llanowar Elves",
	"artifact":     "Ornithopter",
	"land":         "Forest",
	"enchantment":  "Glorious Anthem",
	"planeswalker": "Jace Beleren",
	"goblin":       "Goblin Piker",
	"frog":         "Anurid Murkdiver",
	"snail":        "Skullcap Snail",
	"rat":          "Bog Rats",
	"room":         "Bottomless Pool",
}

// sacTokenBases are subtype bases that name a token kind rather than a card
// type or a printable subtype, so no fixture card pays them. They are listed
// so the census groups them as a token cost rather than an unmodelled filter.
var sacTokenBases = map[string]bool{"food": true, "treasure": true, "clue": true}

// sacFilterFixture returns the fixture card that pays tok's Sac cost. ok is
// false for a self-sacrifice (handled by sacSelf), a token cost, an attached
// or status-qualified filter, and any unmodelled filter.
func sacFilterFixture(tok string) (string, bool) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return "", false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return "", false
	}
	if sacSelf(tok) {
		return "", false
	}
	// The count must be exactly one: the template places one permanent. A
	// Sac<2/Artifact> (or an announced Sac<X/Artifact>) needs two or an
	// announced number, which is a distinct, narrower gap (sacGapClass).
	if n, err := strconv.Atoi(strings.TrimSpace(parts[0])); err != nil || n != 1 {
		return "", false
	}
	// Each ';'-separated clause is an alternative; the first cellable one
	// wins. Type phrases are left in their printed order, so the choice is
	// deterministic.
	for _, alt := range strings.Split(parts[1], ";") {
		if card, ok := sacFilterClause(alt); ok {
			return card, true
		}
	}
	return "", false
}

// sacFilterClause maps one Sac filter alternative to a fixture. A qualifier
// that names a token, an attachment or a status has no plain fixture; a '+'
// modifier (the cmcEQX value gate) does not change which card pays the cost,
// so it is dropped rather than rejecting an otherwise cellable filter.
func sacFilterClause(alt string) (string, bool) {
	alt = strings.ToLower(strings.TrimSpace(alt))
	if i := strings.IndexByte(alt, '+'); i >= 0 {
		alt = alt[:i]
	}
	if strings.Contains(alt, ".token") || strings.Contains(alt, ".attached") ||
		strings.Contains(alt, ".issuspected") {
		return "", false
	}
	base := alt
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if sacTokenBases[base] {
		return "", false
	}
	card, ok := sacFixtureCards[base]
	return card, ok
}

// sacFixtureSupported reports whether a Sac token is cellable (self or a
// fixture the table names). It is activationCost's admission test.
func sacFixtureSupported(tok string) bool {
	if sacSelf(tok) {
		return true
	}
	_, ok := sacFilterFixture(tok)
	return ok
}

// sacGapClass names an unsupported Sac token by cause, so the activate census
// groups a narrowed remainder rather than one opaque Sac<...> bucket. A
// self-sacrifice never reaches here (it is cellable); a token, an attachment,
// an announced X, a multi-count and any remaining filter shape are distinct.
func sacGapClass(tok string) string {
	payload, ok := bracketPayload(tok)
	if !ok {
		return "Sac<malformed>"
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return "Sac<malformed>"
	}
	filter := strings.ToLower(parts[1])
	switch {
	case strings.Contains(filter, ".attached"):
		return "Sac<attached>"
	case strings.Contains(filter, ".token"):
		return "Sac<token>"
	case sacTokenBaseFilter(filter):
		return "Sac<token>"
	case strings.Contains(filter, ".issuspected"):
		return "Sac<unsupported-filter>"
	}
	if _, err := strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
		return "Sac<announced>"
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(parts[0])); n != 1 {
		return "Sac<count>"
	}
	return "Sac<unsupported-filter>"
}

// sacTokenBaseFilter reports whether every alternative names a token kind
// (Food, Treasure, Clue), so the whole cost is a token cost.
func sacTokenBaseFilter(filter string) bool {
	alts := strings.Split(filter, ";")
	for _, alt := range alts {
		base := strings.TrimSpace(alt)
		if i := strings.IndexByte(base, '+'); i >= 0 {
			base = base[:i]
		}
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if !sacTokenBases[base] {
			return false
		}
	}
	return true
}
