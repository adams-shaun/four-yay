package oraclegen

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// XMage poses a "pay the additional cost?" chooseUse for every optional
// additional cost a spell prints (Kicker's KickerAbility, Offspring's
// OffspringAbility, a self-spell OptionalCost static such as waterbend), and
// it does so BEFORE the cast command resolves. gorge instead offers the
// payment as one cast OPTION, so a scenario that declines it leaves XMage's
// ask to be answered by the next queued choice -- an unrelated "yes" on a
// later step (BLB's 17 Offspring creatures, HOB The Eagles Are Coming!,
// TLA's waterbend spells). The fix is to answer the ask explicitly with a
// "no" at the head of the cast step.
//
// XMage only poses the ask when the additional cost alone is payable from the
// cast's mana pool (OptionalAdditionalCostImpl.canPay reads the pool, not the
// base cost still owed), so only an affordable cost gets a "no": a cost the
// pool cannot pay gets no ask, and queuing one anyway would leave an unused
// scripted answer that fails the strict replay.

// CastOptionalCosts lists the optional additional costs a face offers as a
// cast option, in the order XMage's cast poses its asks: the Kicker parts,
// then Offspring, then each self-spell OptionalCost static. Each is a Forge
// mana-cost string; a cost the harness cannot model as mana is omitted.
func CastOptionalCosts(f *cards.Face) []string {
	var out []string
	if p, ok := f.KeywordParam("Kicker"); ok {
		for _, part := range strings.Split(p, ":") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	if p, ok := f.KeywordParam("Offspring"); ok {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	for i := range f.Statics {
		st := &f.Statics[i]
		if !strings.EqualFold(strings.TrimSpace(st.Mode), "OptionalCost") {
			continue
		}
		// Mirror the engine's self-spell OptionalCost shape (rules/statics_costmods.go
		// optionalCostViews): an EffectZone$ All self static on the spell face.
		if !strings.EqualFold(strings.TrimSpace(st.Params["ValidSA"]), "Spell") ||
			!strings.EqualFold(strings.TrimSpace(st.Params["EffectZone"]), "All") ||
			!strings.Contains(st.Params["ValidCard"], "Card.Self") {
			continue
		}
		cost := strings.TrimSpace(st.Params["Cost"])
		if n, ok := waterbendCost(cost); ok {
			out = append(out, strconv.Itoa(n))
		}
	}
	return out
}

// waterbendCost reads Forge's "Waterbend<N>" optional-cost spelling as N
// generic mana (CR 701.65: each artifact or creature tapped pays {1}).
func waterbendCost(cost string) (int, bool) {
	if !strings.HasPrefix(strings.ToLower(cost), "waterbend<") || !strings.HasSuffix(cost, ">") {
		return 0, false
	}
	n, err := strconv.Atoi(cost[len("Waterbend<") : len(cost)-1])
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// OptionalCostCastNo is how many "no" answers the cast step needs for the
// optional additional costs XMage will ask about: one per cost the pool can
// pay (XMage's canPay bound).
func OptionalCostCastNo(f *cards.Face, pool string) int {
	n := 0
	for _, cost := range CastOptionalCosts(f) {
		if poolPaysCost(pool, cost) {
			n++
		}
	}
	return n
}

// PrependCastNo puts n "no" answers at the head of the named card's cast
// step, so XMage's optional-cost chooseUse reads a decline rather than the
// next queued choice.
func PrependCastNo(xans [][]XAnswer, sc Scenario, card string, n int) [][]XAnswer {
	if n <= 0 || len(sc.Steps) == 0 {
		return xans
	}
	out := xans
	if out == nil {
		out = make([][]XAnswer, len(sc.Steps))
	}
	for i, st := range sc.Steps {
		if st.Op != "cast" || st.Card != "p0:"+card {
			continue
		}
		if i >= len(out) {
			break
		}
		no := make([]XAnswer, n)
		for j := range no {
			no[j] = XAnswer{Seat: 0, Kind: "choice", Value: "no"}
		}
		out[i] = append(no, out[i]...)
		break
	}
	return out
}

// poolPaysCost reports whether a pool of mana letters (as oraclegen spells it:
// C for generic/colorless, WUBRG for coloured) can pay a Forge mana cost. It
// mirrors XMage's ManaCostsImpl.canPay, which checks each symbol separately
// against the pool: a generic or colorless symbol is always payable, a
// coloured pip needs its colour present (duplicates reuse the same check).
func poolPaysCost(pool, cost string) bool {
	cost = strings.TrimSpace(cost)
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return true
	}
	for _, sym := range strings.Fields(cost) {
		switch {
		case strings.Trim(sym, "0123456789") == "": // generic
		case sym == "C" || sym == "0": // colorless
		case len(sym) == 1 && strings.Contains("WUBRG", sym):
			if !strings.Contains(pool, sym) {
				return false
			}
		case len(sym) == 2 && strings.Contains("WUBRG", sym[:1]) && strings.Contains("WUBRG", sym[1:]):
			// hybrid {W/B}: either half is enough
			if !strings.Contains(pool, sym[:1]) && !strings.Contains(pool, sym[1:]) {
				return false
			}
		case len(sym) == 2 && sym[0] == '2' && strings.Contains("WUBRG", sym[1:]):
			if !strings.Contains(pool, sym[1:]) {
				return false
			}
		case strings.Contains(sym, "/"):
			h := strings.SplitN(sym, "/", 2)
			if strings.EqualFold(h[1], "P") {
				continue // phyrexian: 2 life is always payable
			}
			if len(h[0]) == 1 && strings.Contains(pool, h[0]) {
				continue
			}
			if len(h[1]) == 1 && strings.Contains(pool, h[1]) {
				continue
			}
			return false
		default:
			return false
		}
	}
	return true
}
