package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
)

// This file is the ONE home for the option list a ChooseNumber ask ranges
// over (task cli-20260923T060000Z-choose-number). Before it, the number list
// existed only inline in rules' as-enters ask (rules/cast.go etbOptions'
// "number" arm), so a mid-resolution ChooseNumber had no list at all and
// silently recorded 0. Here the list is named once; the asking primitive
// (effects/choose.go effChooseNumber) and the as-enters ask build their
// options from this function, so the two asks can never disagree.

// chooseNumberMaxOffer is the inclusive upper bound of the deterministic
// number list, matching the historical as-enters number ask (0..12). It is a
// build default, NOT the card's own range: Max$ (a dynamic or literal bound
// such as a mana pool or energy total) is owned by a separate ticket and
// remains unread here, so every ChooseNumber currently offers this fixed
// list. The bound is deliberately finite so the ask is answerable by a list
// pick; an unbounded card (Void's bare "Choose a number") stays a real ask
// over the values a game can plausibly need rather than a silent 0.
const chooseNumberMaxOffer = 12

// NumberChoices returns the deterministic number option list a ChooseNumber
// ask offers: 0..chooseNumberMaxOffer inclusive, in ascending order, each
// option carrying its value in Amount (the same "number" wire shape the
// as-enters ask already emitted) so the answer needs no label parsing.
func NumberChoices() []decision.Option {
	return boundedNumberChoices(0, chooseNumberMaxOffer)
}

// chooseNumberBoundedCeiling caps the option list a card's own explicit bound
// may build (Max$ resolves through the count evaluator, so an SVar-driven
// bound can name a value that reads huge on an unusual board): a list-pick ask
// ranging over more values than this is not a question this wire shape can
// honestly pose, and the ask takes the loud fail-closed fallback instead of a
// kilo-option list. Every measured corpus bound (literals 3..20, SVar energy
// and life totals) sits far below it.
const chooseNumberBoundedCeiling = 1000

// chooseNumberAsk derives the option list and prompt the mid-resolution
// ChooseNumber ask (effects/choose.go effChooseNumber) offers, reading the
// card's own bound parameters here so the ask has ONE home for them (task
// bounded1, absorbing agent-20260922T091245Z-d777f1b6):
//
//   - Max$ names the inclusive upper bound of the offered list, through the
//     ordinary parameter grammar (effects/count.go NumResolved): a literal
//     (Max$ 5), an SVar name (Max$ Max with SVar:Max:Count$YourCountersEnergy,
//     Rampaging Aetherhood and Territorial Aetherkite), or an inline Count$
//     expression (Max$ Count$YourCountersEnergy, Pia Nalaar, Chief Mechanic,
//     Localized Destruction, Aether Refinery) — resolved against the current
//     resolution context, so the bound is the ASKING controller's actual
//     value at ask time. NumResolvedStrict honours the SVar body's verdict,
//     so an unmodelled body fails closed rather than enforcing a fake 0.
//   - Min$ names the inclusive lower bound the same way (corpus: only small
//     literals, 22 lines), through the same strict resolver — an unmodelled
//     SVar body behind Min$ fails closed like Max$'s, never a fake floor.
//     The default lower bound is 0, deliberately: the
//     ask keeps the card's zero/optional semantics — a "you may pay" carrier
//     answers 0 to decline (ChooseAnyNumber$ True's own spelling of the same
//     shape) — so the bound work never makes a choice positive by assumption.
//   - ListTitle$ is the prompt the chooser sees, verbatim; the historical
//     "Choose a number" stays the fallback when the card names none.
//
// Without a Max$ the list stays the historical fixed 0..chooseNumberMaxOffer,
// unless Min$ sets a floor (then the same default ceiling is retained). The
// as-enters ask (rules/cast.go etbOptions' number arm) still shares its
// unchanged no-argument NumberChoices list.
//
// ok=false reports a bound the card states but this context cannot honour: an
// unresolvable Max$/Min$ expression, a lower bound above the upper one, a
// negative upper bound, or an upper bound past chooseNumberBoundedCeiling.
// The caller keeps the loud fail-closed fallback (a Note naming the
// parameter, then the deterministic first-legal-value emit) rather than
// offering a list that could violate the bound — the sibling ChooseColor
// exotic-shape convention (chooseColorOptions' askable=false), not a silent
// default.
func chooseNumberAsk(h Host, c *Ctx, sa *cards.SA) (opts []decision.Option, prompt string, ok bool) {
	prompt = "Choose a number"
	if title := strings.TrimSpace(sa.Params["ListTitle"]); title != "" {
		prompt = title
	}
	hasMax := sa.HasParam(cards.PKMax)
	if !hasMax || strings.TrimSpace(sa.ParamStr(cards.PKMax)) == "" {
		lo := int32(0)
		if sa.HasParam(cards.PKMin) {
			n, resolvable := NumResolvedStrict(h, c, sa, "Min", 0)
			if !resolvable || n > chooseNumberMaxOffer {
				return nil, prompt, false
			}
			if n > 0 {
				lo = n
			}
		}
		return boundedNumberChoices(lo, chooseNumberMaxOffer), prompt, true
	}
	lo := int32(0)
	if sa.HasParam(cards.PKMin) {
		n, resolvable := NumResolvedStrict(h, c, sa, "Min", 0)
		if !resolvable {
			return nil, prompt, false
		}
		// A negative Min is no constraint the list can carry (the option values
		// are counts); 0 stays the floor. A positive Min is the card's own
		// floor and MUST be honoured, never padded down to 0.
		if n > 0 {
			lo = n
		}
	}
	hi, resolvable := NumResolvedStrict(h, c, sa, "Max", 0)
	if !resolvable || hi < 0 || lo > hi || hi > chooseNumberBoundedCeiling {
		return nil, prompt, false
	}
	return boundedNumberChoices(lo, hi), prompt, true
}

// boundedNumberChoices builds the ascending lo..hi option list in the same
// wire shape NumberChoices uses (Kind "number", the value on Amount, the
// label the printed number), so an answered option needs no label parsing and
// the two builders can never disagree about what a number answer carries.
func boundedNumberChoices(lo, hi int32) []decision.Option {
	out := make([]decision.Option, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		out = append(out, decision.Option{Index: len(out), Kind: "number", Label: strconv.Itoa(int(i)), Amount: int(i)})
	}
	return out
}
