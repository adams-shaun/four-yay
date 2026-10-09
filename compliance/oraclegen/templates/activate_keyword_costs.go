package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// Keyword-shaped activation costs (ticket levelb-activate-keyword-costs):
// Waterbend<N|X> (generic mana in the pool: waterbend lets a player tap
// artifacts and creatures for {1}, it never requires it), XMin<N> (the
// lowest legal X), PayLife<X>, Blight<N>, Forage, Exert<1/...> and a
// non-loyalty AddCounter<N/KIND> on the source. All of them are payable
// from the ordinary fixture plus the one extra permanent or graveyard the
// shared tables below name.

// blightFixture is the vanilla creature a Blight<N> cost is paid with (N <= 2):
// a 3/3 survives two -1/-1 counters.
const blightFixture = "Hill Giant"

// forageFixtures are the three graveyard cards a Forage cost exiles.
var forageFixtures = []string{"Grizzly Bears", "Wastes", "Sol Ring"}

// activationX is the X every X-bearing activation cost is announced with: the
// XMin<N> floor when the cost has one, else 1 for the costs whose X also
// prices a life or waterbend payment (PayLife<X>, Waterbend<X>) or removes
// counters (an announced SubCounter<X/Kind> / RemoveAnyCounter<X/Kind>, whose
// X the fixture funds with one counter). It is 0 for a plain X cost, which
// keeps PoolFor's legacy X.
func activationX(cost string) int {
	toks := costTokens(cost)
	for _, tok := range toks {
		if n, ok := xMinFloor(tok); ok {
			return n
		}
	}
	for _, tok := range toks {
		if tok == "PayLife<X>" || tok == "Waterbend<X>" || announcedSourceCounterX(tok) {
			return 1
		}
	}
	return 0
}

// xMinFloor reads the N of an XMin<N> token (the Forge spelling is XMin1).
func xMinFloor(tok string) (int, bool) {
	rest, ok := strings.CutPrefix(tok, "XMin")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 0
}

// keywordCostMana classifies one keyword-shaped token. handled is false for a
// token this file does not own; mana is the Forge mana string it adds to the
// pool ("" for none).
func keywordCostMana(tok string, x int) (mana string, handled bool) {
	head := tok
	if i := strings.IndexByte(tok, '<'); i >= 0 {
		head = tok[:i]
	}
	if _, ok := xMinFloor(tok); ok {
		return "", true
	}
	payload, bracketed := bracketPayload(tok)
	switch head {
	case "Waterbend":
		if payload == "X" && x > 0 {
			return strconv.Itoa(x), true
		}
		if n, err := strconv.Atoi(payload); err == nil && n > 0 {
			return strconv.Itoa(n), true
		}
	case "PayLife":
		return "", payload == "X" && x > 0
	case "Blight":
		n, err := strconv.Atoi(payload)
		return "", err == nil && n >= 1 && n <= 2
	case "Forage":
		return "", !bracketed
	case "Exert":
		fields := strings.Split(payload, "/")
		return "", bracketed && len(fields) >= 2 && fields[0] == "1" &&
			(strings.EqualFold(fields[1], "CARDNAME") || strings.EqualFold(fields[1], "NICKNAME"))
	case "AddCounter":
		fields := strings.Split(payload, "/")
		if len(fields) != 2 || loyaltyCounter(tok) {
			return "", false
		}
		n, err := strconv.Atoi(fields[0])
		return "", err == nil && n >= 1 && strings.TrimSpace(fields[1]) != ""
	}
	return "", false
}

// substituteX rewrites the bare X mana symbols of a mana token list to the
// announced value, so PoolFor prices X=x exactly. A zero x leaves them for
// PoolFor's legacy X.
func substituteX(mana []string, x int) []string {
	if x == 0 {
		return mana
	}
	out := make([]string, len(mana))
	for i, m := range mana {
		out[i] = m
		if m == "X" {
			out[i] = strconv.Itoa(x)
		}
	}
	return out
}

// keywordCostFixtures places what the keyword costs of cost need on p0.
func keywordCostFixtures(p0 *oraclegen.Seat, cost string) {
	for _, tok := range costTokens(cost) {
		head := tok
		if i := strings.IndexByte(tok, '<'); i >= 0 {
			head = tok[:i]
		}
		switch head {
		case "Blight":
			p0.Battlefield = appendFixtureUnique(p0.Battlefield, blightFixture)
		case "Forage":
			for _, name := range forageFixtures {
				p0.Graveyard = appendFixtureUnique(p0.Graveyard, name)
			}
		}
	}
}

// announcesX reports whether the engine asks the activator to announce X for
// this cost: a bare X mana symbol, PayLife<X>, Waterbend<X>, or an announced
// counter-removal part (SubCounter<X/Kind>, RemoveAnyCounter<X/Kind>). An
// XMin<N> floor whose X only counts exiled cards (Craft's ExileCtrlOrGrave
// <X/...>) is read from the cost, never asked.
func announcesX(cost string) bool {
	for _, tok := range costTokens(cost) {
		if tok == "X" || tok == "PayLife<X>" || tok == "Waterbend<X>" || announcedSourceCounterX(tok) {
			return true
		}
	}
	return false
}

// activationXAnswers is the Step.Answers announcing X for an X-announcing cost.
// A Waterbend<X> part first asks which artifacts and creatures to tap (the
// convoke-style ask precedes the X ask), so that ask is declined with an empty
// pick: waterbend lets a player tap permanents for {1}, it never requires it.
func activationXAnswers(cost string) []oraclegen.Answer {
	x := activationX(cost)
	if x == 0 || !announcesX(cost) {
		return nil
	}
	var out []oraclegen.Answer
	if strings.Contains(cost, "Waterbend<X>") {
		out = append(out, oraclegen.Answer{Kind: "choose", Pick: []string{}})
	}
	return append(out, oraclegen.Answer{Kind: "choose", Pick: []string{"X = " + strconv.Itoa(x)}})
}

// permanentCostFixtures are the distinct candidates for a Permanent.Other
// exile filter: plain permanents, or nonland permanents with an activated
// ability when the clause names that qualifier (The Enigma Jewel).
func permanentCostFixtures(clause string) []string {
	if strings.Contains(clause, "nonland") && strings.Contains(clause, "hasability activated") {
		return []string{"Sol Ring", "Llanowar Elves", "Elvish Mystic", "Mind Stone"}
	}
	return []string{"Colossal Dreadmaw", "Sol Ring", "Grizzly Bears", "Wastes"}
}
