// emerge.go implements the Emerge alternative cast (CR 702.118a): "You may
// cast this spell by sacrificing a creature and paying the emerge cost reduced
// by that creature's mana value."
//
// Emerge is a CASTING OPTION, read directly off the K: line exactly like the
// Flash/Flashback/Bestow/Mutate/MayFlashCost family (cards/keywords.go's
// expandKeywords doc names it), so it is registered here in its own file as
// those keywords are. It is NOT a plain alternative-cost substitution: the
// cast must sacrifice a chosen creature AND reduce the emerge cost by that
// creature's mana value, so it cannot ride keywordAltCost's
// "pay the printed parameter in place of the mana cost" path. The pieces:
//
//   - emergeCost resolves the printed K:Emerge parameter to a Cost, cutting
//     Forge's colon-suffixed metadata (`K:Emerge:5 B B:Artifact`) and
//     withholding every cost shape beyond generic and coloured mana.
//   - emergeOfferCost checks the reduced cost for each sacrifice candidate.
//     sacAsk permits only candidates whose own reduced payment is payable,
//     so an offer cannot turn into an unpayable choice.
//   - the offer (rules/legal.go's hand walk) adds the "emerged" cast mode,
//     and beginCast's "emerged" arm charges the emerge cost plus the Sac part.
//   - sacAsk applies the chosen creature's mana value as a reduction exactly
//     once, after every Sac part is settled, before the mana window reads
//     pc.cost.
package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:Emerge is implemented as a casting option read
	// directly off the K: line (see this file's doc), registered here exactly
	// as bestow.go and mayflash.go register theirs.
	effects.RegisterNonAPI("kw:Emerge")
}

// emergeCost resolves the printed K:Emerge parameter to the base emerge cost.
// Forge's parameter may carry a colon-suffixed metadata tail
// (`K:Emerge:5 B B:Artifact`), the bestow/mutate colon shape, so everything
// from the first colon is dropped before parsing. Only space-separated generic
// and WUBRGC tokens are admitted; every other parsed component (including
// non-mana, hybrid, snow and life) is withheld rather than partially priced.
func emergeCost(f *cards.Face) (Cost, bool) {
	if f == nil {
		return Cost{}, false
	}
	s, ok := f.KeywordParam("Emerge")
	if !ok {
		return Cost{}, false
	}
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return Cost{}, false
	}
	// Validate the input grammar, not just Unknown: ParseCost understands
	// many real cost parts that Emerge's generic-only reduction does not.
	for token := range strings.FieldsSeq(s) {
		if len(token) == 1 && strings.ContainsAny(token, "WUBRGC") {
			continue
		}
		for _, ch := range token {
			if ch < '0' || ch > '9' {
				return Cost{}, false
			}
		}
	}
	c := ParseCost(s)
	if len(c.Unknown) > 0 || c.X > 0 || c.Life != 0 || c.Snow != 0 || c.Tap ||
		len(c.Hybrid) != 0 || len(c.Phyrexian) != 0 || len(c.Twobrid) != 0 || len(c.HybridPhyrexian) != 0 ||
		len(c.Sac) != 0 || len(c.Discard) != 0 || len(c.SubCounter) != 0 || len(c.AddCounter) != 0 ||
		len(c.Exile) != 0 || len(c.Reveal) != 0 || len(c.RevealChosen) != 0 || len(c.Behold) != 0 ||
		len(c.TapPermanent) != 0 || len(c.Blight) != 0 || c.Forage || len(c.Draw) != 0 ||
		len(c.Energy) != 0 || len(c.LifeX) != 0 || c.LifeHalfUp || len(c.DamageYou) != 0 ||
		len(c.GainLife) != 0 ||
		len(c.Return) != 0 || len(c.PutToLib) != 0 || len(c.MoveToGrave) != 0 || len(c.Mill) != 0 ||
		len(c.Evidence) != 0 || len(c.RollDice) != 0 {
		return Cost{}, false
	}
	return c, true
}

// emergeSacrificePart is the mandatory additional cost every emerge cast pays:
// sacrifice one creature (CR 702.118a). It is a CostPart so the ordinary Sac
// machinery (sacAsk, nonManaCastable, sacrificeCostCandidates) enforces it
// without a parallel path.
func emergeSacrificePart() CostPart {
	return CostPart{N: 1, Spec: "Creature"}
}

// emergeBase is shared by offer and payment. The additional creature is the
// FIRST Sac part, even when the spell's own SpellAbility prints another cost.
func emergeBase(f *cards.Face) (Cost, bool) {
	base, ok := emergeCost(f)
	if !ok {
		return Cost{}, false
	}
	base.Sac = append(base.Sac, emergeSacrificePart())
	return pay.WithSpellAbilityExtras(f, base), true
}

// emergeCandidateCost applies ONLY the mandatory Emerge creature's mana value.
func (e *Engine) emergeCandidateCost(base Cost, oid state.ObjID) Cost {
	o := e.G.Obj(oid)
	if o == nil || o.Face() == nil {
		return base
	}
	return reduceGeneric(base, o.Face().ManaValue())
}

// emergeOfferCost checks each candidate against the caller's actual (or
// hypothetical) pool. A best-case price alone must not authorize a smaller
// creature whose own payment is unaffordable. The payment ask repeats this
// candidate-specific constraint against the live pool.
func (e *Engine) emergeOfferCost(p state.PlayerID, id state.ObjID, f *cards.Face, payable func(Cost) bool) (Cost, bool) {
	base, ok := emergeBase(f)
	if !ok {
		return Cost{}, false
	}
	best := int32(-1)
	var priced Cost
	for _, oid := range pay.SacrificeCostCandidates(asPayer(e), p, id, emergeSacrificePart(), false) {
		o := e.G.Obj(oid)
		if o == nil || o.Face() == nil {
			continue
		}
		candidate := e.emergeCandidateCost(base, oid)
		if payable(candidate) && o.Face().ManaValue() > best {
			best, priced = o.Face().ManaValue(), candidate
		}
	}
	return priced, best >= 0
}

// At the sacrifice ask use the same modifier and pool feasibility as the
// offer, but the pending cast's captured modifiers (the card is on the stack
// now). Only mana varies by candidate; the common non-mana parts were checked
// at the offer and are settled independently by sacAsk.
func (e *Engine) emergeSacPayable(pc *pendingCast, oid state.ObjID) bool {
	candidate := e.emergeCandidateCost(pc.cost, oid)
	delve := int32(0)
	if e.hasKeywordH(pc.card, kwhDelve) {
		delve = int32(len(e.G.Zone(state.ZGraveyard, pc.player)))
	}
	return e.manaFeasiblePriced(pc.player, pc.card, false, candidate, pc.mods, pc.taxGeneric, delve, nil)
}

// applyEmergeReduction folds the actual chosen sacrifice's mana value into the
// emerge cast's cost exactly once. Called from sacAsk after every Sac part has
// been settled and before the mana window reads pc.cost, so the reduction is
// part of the cost the payment charges while still composing under the CR
// 601.2f modifiers manaToPay applies afterwards. Idempotent through emergeDone
// so the continueCast re-entries a suspended mana window makes cannot subtract
// twice.
func (e *Engine) applyEmergeReduction(pc *pendingCast) {
	if !pc.emerge || pc.emergeDone {
		return
	}
	pc.cost = e.emergeCandidateCost(pc.cost, pc.emergeSac)
	pc.emergeDone = true
}

// reduceGeneric subtracts n mana from c's GENERIC component only, floored at
// zero. A mana-value reduction (CR 702.118a's Emerge, like CR 118.7's
// cost-reduction effects generally) can only reduce the generic amount of a
// cost: the coloured pips are requirements the spell still carries, so
// {5}{U}{U} reduced by mana value 10 is {U}{U}, never zero. Coloured pips
// and non-mana parts are left untouched. Unsupported printed Emerge cost shapes
// are withheld by emergeCost before this reducer can be reached.
func reduceGeneric(c Cost, n int32) Cost {
	if n <= 0 {
		return c
	}
	if c.Generic < n {
		c.Generic = 0
		return c
	}
	c.Generic -= n
	return c
}
