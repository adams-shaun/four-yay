// webslinging.go implements the Web-slinging alternative cast (CR
// 702.186a-style, Marvel's Spider-Man): "You may cast this spell for <cost>
// if you also return a tapped creature you control to its owner's hand."
//
// Web-slinging is a cost SUBSTITUTION (the printed web-slinging cost replaces
// the mana cost, the Evoke/Miracle shape) composed with a mandatory
// ADDITIONAL cost -- returning a tapped creature you control to its owner's
// hand -- modelled as a Return<1/Creature.YouCtrl+tapped> part and settled by
// the cast flow's ordinary Return machinery: nonManaCastable censuses the
// candidates at the offer gate (an option that cannot be paid is never
// offered, the offerCastable ruling), returnAsk asks which permanent, and
// payCast moves it to its OWNER's hand beside the other payments. The pieces:
//
//   - webSlingingCosts is the ONE cost reader the offer and the charge call,
//     the blitzCosts shape: the printed K:Web-slinging parameter (and any
//     layer-6 AddKeyword$ grant's parameter, the Peter Parker, Amazing
//     Spider-Man shape) resolved off the DERIVED keyword list with the
//     stack-zone override, so offer and charge cannot drift.
//   - the offer (rules/legal.go's hand walk) adds the "web-slinging" cast
//     mode gated on offerCastable.
//   - beginCast's "web-slinging" arm charges the same composed cost.
//   - the pay-time CastInfo carries state.FlagWebSlinged (modeFlags), the
//     provenance the Card.Self+webSlinged filter predicate reads
//     (rules/cast_provenance.go's webSlingedAdmits) -- Spiders-Man, Heroic
//     Horde's ETB trigger. The flag is a CastProvenanceFlag, so a stack copy
//     never inherits it (CR 707.10).
//
// The keyword itself has no post-resolution rider; every carrier's rider is
// its own script line keyed on the flag.

package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:Web-slinging is implemented as a cast option
	// read directly off the K: line (see this file's doc), registered here
	// exactly as emerge.go and mayflash.go register theirs.
	effects.RegisterNonAPI("kw:Web-slinging")
}

// webSlingingReturnSpec is the keyword's mandatory additional cost: a tapped
// creature the caster controls returned to its owner's hand. The Return cost
// machinery reads it at all three stages (nonManaCastable's offer gate,
// returnAsk, payCast's settle): costCandidates' controller-scoped
// battlefield scan IS the "you control", and "+tapped" is the ordinary
// filter property, so an untapped creature can never pay and an
// opponent's creature is never offered.
const webSlingingReturnSpec = "Creature.YouCtrl+tapped"

// webSlingingReturnExtra is the composed cost's additional-cost arm.
func webSlingingReturnExtra() Cost {
	return Cost{Return: []CostPart{{N: 1, Spec: webSlingingReturnSpec}}}
}

// webSlingingCosts is the shared offer/charge reader for every printed or
// granted Web-slinging instance, the blitzCosts shape: each derived
// Web-slinging entry prices its own composed cost (the colon parameter as
// the substituted mana cost, plus the mandatory Return part). Distinct modes
// preserve separate costs when a printed keyword and a grant coexist; the
// grant's trailing spell filter is resolved against the proposed spell
// before it is offered, and the CardManaCost placeholder expands to the
// proposed card's own mana cost, exactly the escape/mayhem grant convention.
func (e *Engine) webSlingingCosts(p state.PlayerID, id state.ObjID) []struct {
	mode string
	cost Cost
} {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	if !e.stackKeywordPossibleH(id, kwhWebSlinging) {
		return nil
	}
	var out []struct {
		mode string
		cost Cost
	}
	wsIndex := 0
	for _, keyword := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(keyword), "Web-slinging") {
			continue
		}
		wsIndex++
		raw := ""
		if i := strings.IndexByte(keyword, ':'); i >= 0 {
			raw = strings.TrimSpace(keyword[i+1:])
		}
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) == 2 {
			spec, admits := e.castProvenanceAdmitsWindow(parts[1], id, p, true)
			if !admits {
				continue
			}
			sc := e.specCtx(0, p)
			sc.AsStack = true
			if !e.matchesSpec(spec, id, sc) {
				continue
			}
		}
		var toks []string
		for _, tok := range strings.Fields(parts[0]) {
			if strings.EqualFold(tok, "CardManaCost") {
				toks = append(toks, strings.Fields(o.Face().ManaCost)...)
			} else {
				toks = append(toks, tok)
			}
		}
		c := ParseCost(strings.Join(toks, " "))
		if len(c.Unknown) != 0 {
			continue
		}
		mode := "web-slinging"
		if wsIndex > 1 {
			mode = fmt.Sprintf("webslinged_grant_%d", wsIndex)
		}
		out = append(out, struct {
			mode string
			cost Cost
		}{mode, c.Plus(webSlingingReturnExtra())})
	}
	return out
}
