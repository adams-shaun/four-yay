package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
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
// '.') to the battlefield fixtures that pay it, in preference order: a count of
// N takes the first N names, and appendFixtureUnique dedupes a name, so each
// base lists N DISTINCT vanilla, no-ETB cards. The base is deliberately
// coarse: Creature.Other, Creature.IsSuspected and a bare Creature all read
// "creature", and a qualifier that makes a plain fixture illegal (a token, an
// attachment, a suspected status) is rejected by sacFilterClause before the
// lookup. Every name is a card the corpus carries and XMage's card database
// carries; the host pass confirms the XMage side (spec hypothesis H4).
var sacFixtureCards = map[string][]string{
	"creature":     {"Llanowar Elves", "Grizzly Bears", "Elvish Mystic", "Nessian Asp", "Craw Wurm", "Siege Wurm"},
	"artifact":     {"Ornithopter", "Sol Ring", "Arcane Signet"},
	"land":         {"Forest", "Island", "Mountain", "Swamp", "Plains"},
	"enchantment":  {"Glorious Anthem", "Honor of the Pure", "Intangible Virtue"},
	"planeswalker": {"Jace Beleren"},
	"goblin":       {"Goblin Piker", "Mons's Goblin Raiders", "Raging Goblin"},
	"frog":         {"Anurid Murkdiver"},
	"snail":        {"Skullcap Snail"},
	"rat":          {"Bog Rats", "Typhoid Rats", "Plague Rats"},
	"room":         {"Bottomless Pool"},
}

// sacTokenBases are subtype bases that name a token kind rather than a card
// type or a printable subtype, so no fixture card pays them. They are listed
// so the census groups them as a token cost rather than an unmodelled filter.
var sacTokenBases = map[string]bool{"food": true, "treasure": true, "clue": true}

// sacFilterFixtures returns the fixture cards that pay tok's Sac cost: as
// many distinct names as the count asks for. ok is false for a
// self-sacrifice (handled by sacSelf), a token cost, an attached or
// status-qualified filter, an announced count, a count above the table and
// any unmodelled filter.
func sacFilterFixtures(tok string) ([]string, bool) {
	payload, ok := bracketPayload(tok)
	if !ok {
		return nil, false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return nil, false
	}
	if sacSelf(tok) {
		return nil, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n < 1 {
		return nil, false
	}
	// Each ';'-separated clause is an alternative; the first cellable one
	// wins. Type phrases are left in their printed order, so the choice is
	// deterministic.
	for _, alt := range strings.Split(parts[1], ";") {
		if cards, ok := sacFilterClause(alt); ok && len(cards) >= n {
			return cards[:n], true
		}
	}
	return nil, false
}

// sacFilterFixture is sacFilterFixtures for a single-permanent cost.
func sacFilterFixture(tok string) (string, bool) {
	cards, ok := sacFilterFixtures(tok)
	if !ok || len(cards) != 1 {
		return "", false
	}
	return cards[0], true
}

// sacFilterClause maps one Sac filter alternative to a fixture. A qualifier
// that names a token, an attachment or a status has no plain fixture; a '+'
// modifier (the cmcEQX value gate) does not change which card pays the cost,
// so it is dropped rather than rejecting an otherwise cellable filter.
// sacCostNamesRoom reports whether a Sac token's filter names the Room card
// type in any of its ';'-separated alternatives. The base word is extracted
// exactly as sacFilterClause extracts it, so the two agree on what "Room" a
// cost can mean.
func sacCostNamesRoom(tok string) bool {
	payload, ok := bracketPayload(tok)
	if !ok {
		return false
	}
	parts := strings.Split(payload, "/")
	if len(parts) < 2 {
		return false
	}
	for _, alt := range strings.Split(parts[1], ";") {
		alt = strings.ToLower(strings.TrimSpace(alt))
		if i := strings.IndexByte(alt, '+'); i >= 0 {
			alt = alt[:i]
		}
		base := alt
		if i := strings.IndexByte(base, '.'); i >= 0 {
			base = base[:i]
		}
		if base == "room" {
			return true
		}
	}
	return false
}

// observedSacPick is the XMage answer for one observed Sac cost pick. A Room
// is the one paying permanent XMage names by its whole split name ("Bottomless
// Pool // Locker Room") while gorge's scenario ref spells the front face, so
// the plain-name pick can never match (XMage's haveSameNames is an exact
// equals) and the scenario dies on "Found wrong choice command". The
// exact-ref alias routes the pick by object identity instead: the driver binds
// the front-half ref of every split object at setup. Keyed on the COST naming
// Room, never on a card name -- a Sac<...Room...> filter only permits Rooms,
// so its observed pick is a Room, and every non-Room Sac pick keeps the
// plain-name form the other cards agree on.
func observedSacPick(tok string, d rules.OracleDecision, i int) string {
	if sacCostNamesRoom(tok) && i < len(d.PickRefs) && i < len(d.PickRefsInexact) &&
		!d.PickRefsInexact[i] && !strings.Contains(d.PickRefs[i], ":token:") &&
		oraclegen.IsScenarioRefShaped(d.PickRefs[i]) {
		return "@" + d.PickRefs[i]
	}
	return observedCostPick(d, i)
}

func sacFilterClause(alt string) ([]string, bool) {
	alt = strings.ToLower(strings.TrimSpace(alt))
	if i := strings.IndexByte(alt, '+'); i >= 0 {
		alt = alt[:i]
	}
	if strings.Contains(alt, ".token") || strings.Contains(alt, ".attached") ||
		strings.Contains(alt, ".issuspected") {
		return nil, false
	}
	base := alt
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if sacTokenBases[base] {
		return nil, false
	}
	card, ok := sacFixtureCards[base]
	return card, ok
}

// sacFixtureSupported reports whether a Sac token is cellable (self, a
// fixture the table names, or a token a maker prelude produces). It is
// activationCost's admission test.
func sacFixtureSupported(tok string) bool {
	if sacSelf(tok) || tokenCostSupported(tok) {
		return true
	}
	_, ok := sacFilterFixtures(tok)
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
