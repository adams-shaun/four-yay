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

// poolPaysCost counts actual mana letters (C is colorless, WUBRG coloured).
// Reserve fixed pips first, then spend the remaining mana on generic costs.
// Hybrid choices are tried both ways so a scarce colour is not consumed by
// a flexible pip when another pip needs it. An unknown symbol fails closed.
func poolPaysCost(pool, cost string) bool {
	cost = strings.TrimSpace(cost)
	if cost == "" || strings.EqualFold(cost, "no cost") {
		return true
	}
	var mana [6]int // W U B R G C
	for _, c := range pool {
		i := strings.IndexRune("WUBRGC", c)
		if i < 0 {
			return false
		}
		mana[i]++
	}
	syms := strings.Fields(cost)
	var pay func(int, int) bool
	pay = func(at, generic int) bool {
		if at == len(syms) {
			for _, n := range mana {
				generic -= n
			}
			return generic <= 0
		}
		sym := syms[at]
		if n, err := strconv.Atoi(sym); err == nil && n >= 0 {
			return pay(at+1, generic+n)
		}
		// Each choice is either a fixed mana colour, a generic payment
		// (encoded as '2'), or life ('P').
		choices := []string{sym}
		if len(sym) == 2 && strings.ContainsRune("WUBRG", rune(sym[0])) && strings.ContainsRune("WUBRG", rune(sym[1])) {
			choices = []string{sym[:1], sym[1:]}
		} else if len(sym) == 2 && sym[0] == '2' && strings.ContainsRune("WUBRG", rune(sym[1])) {
			choices = []string{sym[1:], "2"}
		} else if strings.Contains(sym, "/") {
			choices = strings.Split(sym, "/")
			if len(choices) != 2 || (choices[1] == "P" && (len(choices[0]) != 1 || !strings.Contains("WUBRGC", choices[0]))) {
				return false
			}
		} else if sym == "P" {
			return false
		}
		for _, option := range choices {
			if option == "P" { // Phyrexian life payment
				if pay(at+1, generic) {
					return true
				}
				continue
			}
			if option == "2" {
				if pay(at+1, generic+2) {
					return true
				}
				continue
			}
			if len(option) != 1 {
				continue
			}
			i := strings.IndexByte("WUBRGC", option[0])
			if i < 0 || mana[i] == 0 {
				continue
			}
			mana[i]--
			ok := pay(at+1, generic)
			mana[i]++
			if ok {
				return true
			}
		}
		return false
	}
	return pay(0, 0)
}
