package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// Spree (CR 702.171) and Tiered are casting-option keywords: like Kicker or
// Surge they are read directly by rules rather than expanded into a trigger,
// replacement or activated ability, so there is no cards/keywords.go case.
// What they carry is a modal spell ("SP$ Charm") whose per-mode SVar bears a
// ModeCost$ -- the additional cost a chosen mode charges on top of the
// printed mana cost. Registering the heads keeps make report honest about the
// keyword being implemented (the casting-option set kw:Kicker/kw:Surge
// already lives in this package).
func init() {
	effects.RegisterNonAPI("kw:Spree", "kw:Tiered")
}

// modeCost resolves one Charm mode's per-mode additional cost from the SVar
// the mode names (Forge's ModeCost$ on the DB/SP line, e.g. Requisition
// Raid's three `DB$ ... | ModeCost$ 1` modes). The three results are distinct
// on purpose:
//
//   - present=false: the mode carries no ModeCost$ at all -- a free mode.
//   - present=true, ok=true: a ModeCost$ this build can price.
//   - present=true, ok=false: a ModeCost$ is PRINTED but ParseCost cannot
//     model a token in it. This must never read as "free" (a mandatory
//     additional cost silently dropping to zero is a widening), so every
//     caller withholds the mode and the cast cannot select it. Distinguishing
//     the unparseable shape from the absent one is the whole point of the
//     third result -- the pre-review signature collapsed both into ok=false.
func modeCost(f *cards.Face, name string) (Cost, bool, bool) {
	if f == nil {
		return Cost{}, false, false
	}
	sub := cards.ResolveSVar(f.SVars, strings.TrimSpace(name))
	if sub == nil {
		return Cost{}, false, false
	}
	raw := strings.TrimSpace(sub.ParamStr(cards.PKModeCost))
	if raw == "" {
		return Cost{}, false, false
	}
	c := ParseCost(raw)
	if len(c.Unknown) > 0 {
		return Cost{}, true, false
	}
	return c, true, true
}

// modeCostPresent reports whether the mode prints a ModeCost$ this build
// cannot price (present but unparseable). The mode gate withholds such a mode
// so it is never selected; this predicate names that shape at the one place
// the decision is made.
func modeCostUnparseable(f *cards.Face, name string) bool {
	_, present, ok := modeCost(f, name)
	return present && !ok
}

// modeCostTotal sums the ModeCost$ of every chosen mode, one charge per
// occurrence: the exact additional cost a Spree/Tiered cast owes above its
// printed cost (CR 702.171b). A repeated mode (CanRepeatModes$) is charged
// once per pick, so the input is the answered ordered list, not a set. A mode
// with no ModeCost$ contributes nothing. A present-but-unparseable ModeCost$
// is never charged zero here: the mode gate withholds it from the legal set,
// so an unparseable mode cannot appear in names; were it to (a future caller),
// skipping it would be the same fail-open the gate now prevents.
func modeCostTotal(f *cards.Face, names []string) Cost {
	var total Cost
	for _, name := range names {
		if c, present, ok := modeCost(f, name); present && ok {
			total = total.Plus(c)
		}
	}
	return total
}

// modeCostFeasible prices one Spree/Tiered mode's ModeCost$ against the cast
// this mode would join, at CR 601.2b announcement time. It is the per-mode
// leg of the offer gate, NOT a bare pool check: the cost it prices is
// pc.cost.Plus(extra) composed through the SAME reducer/increaser snapshot
// the eventual charge (manaToPay) will apply -- pc.mods, the pre-target
// CR 601.2b snapshot beginCast stores -- with the target-dependent-reduction
// retry offerCastableUsing makes, because a mode's own ValidTgts$ gain an
// actual target only later in the transaction. A modifier-blind price would
// withhold a mode the seat can afford after a ReduceCost/SetCost static and,
// when every mode was withheld, drive the whole cast through the min >
// len(legal) no-progress abort: a legal cast denied.
//
// The mana half goes through manaFeasiblePool against the hypothetical
// potential pool pot (a pure read: no mana has been floated yet in CR
// 601.2g), the same over-bound direction legalActionsPriced's expensive-only
// walk uses; every non-mana part is still checked against the REAL state by
// nonManaCastable, so floating mana never buys a sacrifice. The composition
// mirrors offerCastableUsing exactly: base and pc.mods with pc.taxGeneric
// passed separately to the mana half (feasibleAny adds it after the
// composition), and composedOfferCost for the non-mana halt.
func (e *Engine) modeCostFeasible(pc *pendingCast, extra Cost, pot state.Mana) bool {
	if pc == nil || pc.isAbility() {
		return false
	}
	base := pc.cost.Plus(extra)
	scope := spellScope(pc.mode)
	tax := pc.taxGeneric
	delve := int32(0)
	if !pc.isAbility() {
		delve = int32(len(pc.delve))
	}
	typed := e.G.Players[pc.player].ManaUnits()
	mods := pc.mods
	if !e.manaFeasiblePool(pc.player, pc.card, pc.isAbility(), base, mods, tax, delve, pot, typed) {
		// A target-dependent reducer cannot be in the pre-target snapshot, but
		// it may make one legal target choice payable; retry with exactly the
		// potential reductions (offerCastableUsing's own fallback).
		potential, ok := e.potentialCostModsUsing(e.collectCostStatics(), pc.player, pc.card, scope, e.costPotentialTargets(pc.player, pc.card, scope), 0, func(m costMods) bool {
			return e.manaFeasiblePool(pc.player, pc.card, pc.isAbility(), base, m, tax, delve, pot, typed)
		})
		if !ok {
			return false
		}
		mods = potential
	}
	return e.nonManaCastable(pc.player, pc.card, e.composedOfferCost(pc.player, pc.card, base, mods, scope), pc.isAbility(), tapCostSAKind(scope.ab))
}
