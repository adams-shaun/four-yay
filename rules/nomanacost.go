package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// CR 118.6: "Some objects have no mana cost. ... If an object's cost includes
// its mana cost and that object has no mana cost, the object can't be cast
// unless it's cast by paying an alternative cost or cast without paying its
// mana cost." Forge prints this as `ManaCost:no cost` (Gaea's Will, Ancestral
// Vision, Lotus Bloom, Living End, Hypergenesis, Evermind, ...): such cards
// reach the stack only through Suspend, a free-cast effect or an alternative
// cost.
//
// ParseCost reads "no cost" as the empty (free) Cost, which is the right
// PRICE for every free route, but it made every cast mode that pays the
// printed mana cost -- the plain cast and the additional-cost variants built
// on top of it -- offer a no-cost card as castable for {0}. Gaea's Will then
// recast itself from the graveyard under its own MayPlay static forever (a
// livelock the corpus fuzzer found).
//
// The filter runs at legalActionsPriced's single choke point (like
// filterSplitSecondActions), so every cast source -- hand, command zone,
// may-play and the graveyard recasts -- is covered by construction.

// paysPrintedManaCost reports whether a "cast" option in mode pays the cast
// face's PRINTED mana cost (optionally plus additional costs). An
// alternative-cost option (AltCostIndex > 0) and every keyword mode that
// substitutes its own cost (flashback, escape, evoke, suspend, plot_cast,
// ...) is not in the set.
func paysPrintedManaCost(opt *decision.Option) bool {
	if opt.Kind != "cast" || opt.AltCostIndex > 0 {
		return false
	}
	return paysPrintedManaCostSet.Has(opt.Mode)
}

// isNoManaCost reports the printed "no cost" mana cost (CR 118.6 / 202.1b).
func isNoManaCost(mc string) bool {
	return strings.EqualFold(strings.TrimSpace(mc), "no cost")
}

// filterNoManaCostCasts drops every offer that would cast a no-mana-cost
// face by paying its mana cost. A may-play grant that casts WITHOUT paying
// the mana cost (MayPlayWithoutManaCost$) stays offered: that is exactly
// CR 118.6's permitted route.
func (e *Engine) filterNoManaCostCasts(p state.PlayerID, out []decision.Option) []decision.Option {
	// The options are read in place (an Option is a large struct; a ranged
	// copy per option per walk was a measurable share of the filter).
	drop := false
	for i := range out {
		if e.castsNoManaCostByPaying(p, &out[i]) {
			drop = true
			break
		}
	}
	if !drop {
		return out
	}
	kept := out[:0]
	for i := range out {
		if !e.castsNoManaCostByPaying(p, &out[i]) {
			kept = append(kept, out[i])
		}
	}
	for i := range kept {
		kept[i].Index = i
	}
	return kept
}

func (e *Engine) castsNoManaCostByPaying(p state.PlayerID, opt *decision.Option) bool {
	if !paysPrintedManaCost(opt) {
		return false
	}
	o := e.G.Obj(opt.Obj)
	if o == nil || o.Face() == nil || !isNoManaCost(o.Face().ManaCost) {
		return false
	}
	if opt.Mode == "mayplay" {
		if free, ok := e.mayPlayGrant(p, opt.Obj); ok && free {
			return false
		}
	}
	return true
}

var paysPrintedManaCostSet = state.NewNameSet(
	"",
	"mayflash",
	"kicked",
	"kicked1",
	"kicked2",
	"kickedboth",
	"replicated",
	"multikicked",
	"squadded",
	"conspired",
	"buyback",
	"offspring",
	"optionalcost",
	"retrace",
	"jumpstart",
	"warp_recast",
	"mayplay",
)
