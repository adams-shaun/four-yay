// sneak.go implements the Sneak alternative cast (CR 702.190a): "Any time you
// could cast an instant during your declare blockers step, you may cast this
// spell by paying [cost] and returning an unblocked creature you control to
// its owner's hand rather than paying this spell's mana cost." A permanent
// whose sneak cost was paid enters tapped and attacking the same
// player/planeswalker/battle as the returned creature (CR 702.190b).
//
// Sneak is a CAST for an alternative cost, not an activated ability (the
// Ninjutsu distinction, cards/kw_ninjutsu.go), so it lives in the cast
// machinery rather than the hand-zone activated-ability offer:
//
//   - sneakCosts is the ONE cost reader the offer and the charge call (the
//     webSlingingCosts/blitzCosts shape): each derived `Sneak` keyword entry
//     prices its own composed cost -- the colon parameter as the substituted
//     mana cost, plus the mandatory Return<1/Creature.YouCtrl+attacking+
//     unblocked> additional cost (the Ninjutsu cost shape; the
//     attacking+unblocked predicates are what withhold the cast until an
//     attacker is actually unblocked). A layer-6 AddKeyword$ Sneak grant
//     (Ninja Teen's level 3, "Creature cards in your graveyard have sneak
//     {3}{B}") is read off the DERIVED keyword list, so a granted card is
//     offered exactly like a printed one.
//   - the offer (rules/legal.go's hand walk) adds the "sneak" cast mode,
//     confined to the caster's own declare-blockers step by sneakTimingOK.
//   - beginCast's "sneak" arm charges the same composed cost; the ordinary
//     Return machinery (returnAsk, payCast) asks which unblocked attacker
//     pays.
//   - the pay-time CastInfo carries state.FlagSneaked (modeFlags), the
//     provenance the `sneaked` filter predicate reads
//     (effects/filter.go). It is a CastProvenanceFlag, so a stack copy never
//     inherits it (CR 707.10).
//   - the resolution-entry hook (altCast.go's altCostEnter) reads the
//     defender captured when the Return cost was paid off the permanent's
//     Remembered list and places it tapped and attacking (CR 702.190b).
//
// The keyword itself has no post-resolution rider; every carrier's rider is
// its own script line keyed on the `sneaked` predicate.

package rules

import (
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func init() {
	// The coverage census: kw:Sneak is implemented as a cast option read
	// directly off the K: line (see this file's doc), registered here exactly
	// as webslinging.go and mayflash.go register theirs.
	effects.RegisterNonAPI("kw:Sneak")
}

// sneakReturnSpec is the keyword's mandatory additional cost: an unblocked
// attacker the caster controls returned to its owner's hand (CR 702.190a).
// It is the exact Ninjutsu cost shape, so costCandidates' controller-scoped
// battlefield scan is the "you control", and the filter's attacking+unblocked
// properties are what a blocked or non-attacking creature fails.
const sneakReturnSpec = "Creature.YouCtrl+attacking+unblocked"

// sneakReturnExtra is the composed cost's additional-cost arm.
func sneakReturnExtra() Cost {
	return Cost{Return: []CostPart{{N: 1, Spec: sneakReturnSpec}}}
}

// sneakTimingOK is CR 702.190a's window: "any time you could cast an instant
// during your declare blockers step". Only the caster's own declare-blockers
// step qualifies -- Sneak is not offered on an opponent's turn or in any
// other step -- and the window is instant-speed, so a creature printed with
// no Flash is still offered here.
func (e *Engine) sneakTimingOK(p state.PlayerID) bool {
	return e.G.Active == p && e.G.Step == state.StepDeclareBlockers
}

// sneakCosts is the shared offer/charge reader for every printed or granted
// Sneak instance, the webSlingingCosts shape: each derived Sneak entry prices
// its own composed cost (the colon parameter as the substituted mana cost,
// plus the mandatory Return part). The colon parameter may carry a trailing
// rider field (the Ninjutsu/Equip convention), so only the FIRST colon field
// is the cost -- a rider can never leak into the mana cost. Distinct modes
// preserve separate costs when a printed keyword and a grant coexist; a grant
// whose trailing spell filter fails is skipped.
func (e *Engine) sneakCosts(p state.PlayerID, id state.ObjID) []struct {
	mode string
	cost Cost
} {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	if !e.stackKeywordPossibleH(id, kwhSneak) {
		return nil
	}
	var out []struct {
		mode string
		cost Cost
	}
	sneakIndex := 0
	for _, keyword := range e.derivedWith(id, state.ZStack).Keywords {
		if !strings.EqualFold(cardsKeywordHead(keyword), "Sneak") {
			continue
		}
		sneakIndex++
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
		mode := "sneak"
		if sneakIndex > 1 {
			mode = fmt.Sprintf("sneaked_grant_%d", sneakIndex)
		}
		out = append(out, struct {
			mode string
			cost Cost
		}{mode, c.Plus(sneakReturnExtra())})
	}
	return out
}

// sneakDefenderFrom reads the defender captured when a sneak cast's Return
// cost was paid, off the entering permanent's Remembered list (the Choose
// "remembered" entry events/apply.go folds, the same channel Ninjutsu's
// captured defender rides). ok is false for any entry carrying no player.
func sneakDefenderFrom(o *state.Object) (state.PlayerID, bool) {
	if o == nil {
		return 0, false
	}
	for _, rem := range o.Remembered {
		if rem.IsPlayer {
			return rem.Player, true
		}
	}
	return 0, false
}

// sneakEnter is the CR 702.190b entry rider: a permanent whose sneak cost was
// paid enters tapped and attacking the same defender the returned creature
// was attacking. It is called from altCostEnter, which runs for every
// battlefield MoveZone, so the permanent is already on the battlefield and
// the TokenAttacks event's apply sees it there. A sneak cast with no
// captured defender (a malformed path) is left untapped: entering tapped is
// part of the same CR 702.190b sentence, and without a recorded defender we
// cannot place the attack.
func (e *Engine) sneakEnter(id state.ObjID, controller state.PlayerID) {
	o := e.G.Obj(id)
	defender, ok := sneakDefenderFrom(o)
	if !ok {
		return
	}
	e.emit(events.Event{Kind: events.TokenAttacks, Obj: id, Player: controller,
		IDs: []state.ObjID{state.PlayerRef(defender)}, Text: "entered attacking"})
}
