package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// Level-B serves a self-spell OptionalCost static ("As an additional cost to
// cast this spell, you may collect evidence N / blight N / behold a Dragon /
// waterbend {N}") by casting the card WITH the cost paid, so the cost's own
// trace and the spell's "if the additional cost was paid" branch reach the
// snapshots both engines compare. Only the shape below is classified; any
// other OptionalCost static (Reveal, PayLife, Sac, a second param) stays a
// visible gap.

// optionalCostHeads are the cost parts the template can pay: each has a
// fixture and a matching XMage cost selector. ChooseCreatureType rides along
// only for Celestial Reunion, whose compound cost the template skips by name.
var optionalCostHeads = map[string]bool{
	"CollectEvidence": true, "Blight": true, "Behold": true, "Waterbend": true,
	"ChooseCreatureType": true,
}

// optionalCostSelfShape reports whether st is the self-spell OptionalCost
// static whose every cost part is one of optionalCostHeads, carrying exactly
// the EffectZone$/ValidCard$/ValidSA$/Cost$ params (paramsAre ignores Mode$
// and Description$).
func optionalCostSelfShape(st *cards.Static) bool {
	if !strings.EqualFold(st.Mode, "OptionalCost") {
		return false
	}
	if !paramsAre(st, map[string]string{"EffectZone": "All", "ValidCard": "Card.Self", "ValidSA": "Spell"}, "Cost") {
		return false
	}
	toks := CostTokens(st.Params["Cost"])
	if len(toks) == 0 {
		return false
	}
	for _, tok := range toks {
		head, _, _ := strings.Cut(tok, "<")
		if !optionalCostHeads[head] {
			return false
		}
	}
	return true
}

// CostTokens splits a Forge cost string on whitespace, keeping a token's
// `<...>` payload together: Sac<1/CARDNAME/this creature> and
// tapXType<Any/Creature.Other+withTotalPowerGE1> carry spaces a naive Fields
// split would break.
func CostTokens(cost string) []string {
	var out []string
	depth, start := 0, -1
	for i, r := range cost {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ' ', '\t':
			if depth == 0 {
				if start >= 0 {
					out = append(out, cost[start:i])
					start = -1
				}
				continue
			}
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, cost[start:])
	}
	return out
}
