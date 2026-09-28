package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Offspring (CR 702.175) — the optional ADDITIONAL cast cost, and the 1/1
// token copy it creates on entry. Four pieces:
//
//   - offspringCost resolves the printed K:Offspring:<cost> parameter, OR a
//     layer-6 AddKeyword$ Offspring:<cost> grant that reaches the cast spell
//     (Zinnia, Valley's Voice's "Creature spells you cast have offspring
//     {2}"). It reads the DERIVED keyword list with the stack-zone override
//     (derivedWith's atStack), the exact read hasCastConvoke makes, so a
//     granted offspring is priced at the cast offer and the granted cost —
//     never the printed one — is charged.
//   - the offer (rules/legal.go) exposes a distinct "cast (offspring)"
//     option paying base + offspring, the Buyback shape (Offspring is an
//     additional cost, so it is base.Plus(offspring), never a substitution
//     like Bestow/Mutate).
//   - modeFlags marks FlagOffspringPaid, the pay-time provenance the
//     keyword expansion's ETB trigger reads through Count$OffspringPaid.
//   - cards/kw_offspring.go's expansion mints the 1/1 token copy when that
//     count is nonzero.
//
// Unlike Squad (an "any number of times" count ask), Offspring is paid at
// most once and the election is the offer itself, so there is no decision
// ask: Paying is the option, declining is the plain cast option beside it.

// offspringRawParam returns the Offspring keyword parameter reaching the cast
// spell: the parameter of the FIRST derived keyword whose head is Offspring.
// The derived list (derivedWith with the stack-zone override) already
// carries the printed K: line plus every layer-6 AddKeyword$ grant whose
// AffectedZone$ scope reaches the cast, so a single read covers both the
// printed and the Zinnia-granted forms — and the two cannot disagree about
// which cost the cast pays.
func (e *Engine) offspringRawParam(id state.ObjID) (string, bool) {
	if !e.stackKeywordPossibleH(id, kwhOffspring) {
		return "", false
	}
	for _, k := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), "Offspring") {
			continue
		}
		if i := strings.IndexByte(k, ':'); i >= 0 {
			return strings.TrimSpace(k[i+1:]), true
		}
		// A parameterless "Offspring" head carries no cost; treat it as
		// absent so the offer is withheld rather than charged a degraded
		// generic (no corpus carrier prints a bare head, measured
		// 21/21 carry a colon cost).
		return "", false
	}
	return "", false
}

// offspringCost resolves the Offspring additional cost for the cast offer and
// the payment: the derived keyword parameter through ParseCost, withheld
// (ok false) when the parameter is missing or carries a token ParseCost
// cannot model — the replicateCost/bestowCost fail-closed convention, so an
// unpriceable offspring cost never offers a degraded charge. A modelled
// non-mana part is accepted and its payability decided by the ordinary offer
// gate, exactly like any other additional cost.
func (e *Engine) offspringCost(id state.ObjID) (Cost, bool) {
	raw, ok := e.offspringRawParam(id)
	if !ok {
		return Cost{}, false
	}
	c := ParseCost(raw)
	if len(c.Unknown) > 0 {
		return Cost{}, false
	}
	return c, true
}

// hasCastOffspring reports whether the cast spell carries Offspring at all —
// printed or layer-6 granted — through the same derived-keyword read the
// cost resolves through, so the offer gate and the cost stage cannot
// disagree about whether the spell has offspring.
func (e *Engine) hasCastOffspring(id state.ObjID) bool {
	_, ok := e.offspringRawParam(id)
	return ok
}

func init() {
	// Coverage: the keyword head cards/primitive.go derives for every
	// K:Offspring line is now engine-supported (the additional-cost cast
	// offer, the pay-time provenance and the 1/1 copy trigger all live in
	// rules + cards/kw_offspring.go).
	effects.RegisterNonAPI("kw:Offspring")
}
