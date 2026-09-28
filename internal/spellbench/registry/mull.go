package registry

// mull is a deck-aware mulligan decorator: it answers the two shapes of the
// London mulligan decision (rules/mulligan.go) from the seat's OWN hand and
// delegates every other decision to the seat it wraps, unchanged.
//
// # The keep/mulligan rule
//
// The decorator reads the deciding seat's own opening hand (its Hand in the
// projected View -- a hidden zone it owns, so no opponent information is
// touched) and answers "keep" or "mulligan":
//
//   - a hand of 5 or fewer cards is always kept;
//   - a hand of 6 cards is kept when it holds 1..5 lands and at least one
//     spell castable by turn 3 with those lands (colours respected);
//   - a hand of 7 (or more) cards is kept when all three hold: 2..5 lands,
//     at least one spell castable by turn 3 with those lands, and the lands
//     produce at least one colour the hand's spells need.
//
// "Castable by turn 3" is the decorator's own conservative mana model: with
// the hand's lands you have played at most three of them by turn 3, so a
// spell is castable when its mana value is at most min(lands, 3) AND every
// coloured pip it needs has a producing land in that set. It is a heuristic,
// not the engine's cost rules -- hybrid and Phyrexian symbols are counted as
// one pip of each colour they name (harder to satisfy, the safe direction),
// and an "add any colour" land is treated as a source for every colour.
//
// NOTE ON THE LONDON ROUND: rules/mulligan.go's London redraw hands a seat a
// FULL seven on every mulligan (redrawMulligan draws openingHand == 7), so a
// real keep/mulligan ask always presents seven cards and the 6/5 branches
// above are reachable only where a round (or a unit test) presents a short
// hand. The rule is keyed on the hand length the seat can actually see; it
// never guesses a mulligan count from prompt text.
//
// # The bottoming rule
//
// During the bottoming phase the decision offers one "bottom" option per
// kept-hand card. The decorator bottoms exactly Min (== Max) cards, in this
// priority order:
//
//  1. lands in excess of four, so the kept hand keeps 2..4 lands where the
//     hand allows it;
//  2. the most expensive uncastable spells first (mana value descending);
//  3. the most expensive castable spells next;
//  4. only if forced, lands (down to, but never below, two when anything
//     else is left).
//
// # Surface, determinism and error handling
//
// The decorator needs the projected View (the hand lives there), so it is a
// plain seat.Seat even over a seat.BoardSeat inner: it delegates such an
// inner through seat.BoardFromView, the view-shaped half the adapter-parity
// tests pin to BoardFromGame. It keeps Unwrapper so the runner's stats and
// refused-answer fallback still reach the seat underneath, and it forwards
// the payment-plan opt-in unchanged. Every choice is a pure function of the
// view and the decision -- no map iteration reaches an answer (maps are
// lookups; every list is sorted with the option index as the final tiebreak)
// and no randomness is drawn, so no SplitMix64 stream is touched and a
// seed-streamed inner draws exactly what it would have drawn bare. The inner
// seat is consulted exactly once per decision, and the decorator's own pick
// is used only when Decision.Validate accepts it -- a pick that cannot be
// built or is refused falls back to the inner's answer, never to a pass.

import (
	"context"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func init() {
	RegisterDecorator("mull", newMull)
}

// newMull wraps inner in the mull decorator. seed is unused: every mulligan
// answer is deterministic, so no SplitMix64 stream is consumed.
func newMull(inner seat.Seat, _ uint64) seat.Seat {
	return mullSeat{inner: inner}
}

// mullSeat is the decorator's wrapper.
type mullSeat struct {
	inner seat.Seat
}

// UnwrapSeat exposes the wrapped seat (the registry's Unwrapper contract).
func (s mullSeat) UnwrapSeat() seat.Seat { return s.inner }

// WantsPaymentActions forwards the inner's payment-plan opt-in: the wrapper
// offers exactly what inner offers.
func (s mullSeat) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

// Decide consults inner exactly once, then replaces the answer at a mulligan
// decision when the rule can build a valid one. Every other kind is inner's
// answer untouched.
func (s mullSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.delegate(ctx, v, d)
	if err != nil {
		return in, err
	}
	if d.Kind != decision.KMulligan {
		return in, nil
	}
	got, ok := mullAnswer(v, d)
	if !ok {
		return in, nil
	}
	got.Seq, got.Player = d.Seq, d.Player
	if err := d.Validate(got); err != nil {
		// The rule's pick is not a legal answer here: the idea cannot be
		// built, so inner's answer stands (never a pass).
		return in, nil
	}
	return got, nil
}

// delegate hands the decision to inner on the surface inner expects: a
// BoardSeat inner gets a Board built from the view, everyone else the view.
func (s mullSeat) delegate(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	if bs, ok := s.inner.(seat.BoardSeat); ok {
		return bs.DecideBoard(ctx, seat.BoardFromView(v), d)
	}
	return s.inner.Decide(ctx, v, d)
}

// mullAnswer builds the decorator's answer for one KMulligan decision, in
// either the keep/mulligan or the bottoming shape. ok is false when the
// decision cannot be answered from the view (no hand for the deciding seat),
// in which case the caller delegates to inner.
func mullAnswer(v view.View, d decision.Decision) (decision.Intent, bool) {
	if len(d.Options) == 0 {
		return decision.Intent{}, false
	}
	if d.Options[0].Kind == "bottom" {
		return mullBottomAnswer(v, d)
	}
	return mullKeepAnswer(v, d)
}

// mullKeepAnswer decides keep vs mulligan from the seat's own hand.
func mullKeepAnswer(v view.View, d decision.Decision) (decision.Intent, bool) {
	hand, ok := mullHand(v, d.Player)
	if !ok {
		return decision.Intent{}, false
	}
	keep, ok := mullKeep(hand)
	if !ok {
		return decision.Intent{}, false
	}
	kind := "keep"
	if !keep {
		kind = "mulligan"
	}
	if idx, ok := mullOptionKind(d, kind); ok {
		return decision.Intent{Choices: []int{idx}}, true
	}
	// The desired kind is not offered (a seat out of permitted mulligans is
	// offered keep only): keep.
	if idx, ok := mullOptionKind(d, "keep"); ok {
		return decision.Intent{Choices: []int{idx}}, true
	}
	return decision.Intent{}, false
}

// mullKeep applies the keep rule to a hand. ok is false when the hand cannot
// be classified (an empty hand).
func mullKeep(hand []view.CardView) (keep, ok bool) {
	if len(hand) == 0 {
		return false, false
	}
	lands, spells := mullSplit(hand)
	landCount := len(lands)
	switch {
	case len(hand) <= 5:
		return true, true
	case len(hand) == 6:
		return landCount >= 1 && landCount <= 5 && mullHasTurn3Castable(spells, lands), true
	default:
		return landCount >= 2 && landCount <= 5 &&
			mullHasTurn3Castable(spells, lands) &&
			mullLandsProduceNeededColour(spells, lands), true
	}
}

// mullBottomAnswer builds the bottoming pick: exactly Min cards, chosen by
// the bottoming rule.
func mullBottomAnswer(v view.View, d decision.Decision) (decision.Intent, bool) {
	hand, ok := mullHand(v, d.Player)
	if !ok || len(d.Options) == 0 {
		return decision.Intent{}, false
	}
	byObj := make(map[state.ObjID]view.CardView, len(hand)) // lookup only
	for _, cv := range hand {
		byObj[cv.ID] = cv
	}
	var lands, uncastable, castable []mullCandidate
	landCount := 0
	for _, o := range d.Options {
		cv, has := byObj[o.Obj]
		c := mullCandidate{idx: o.Index, land: has && mullIsLand(cv)}
		if has {
			c.cmc = botpolicy.CmcOf(cv.ManaCost)
		}
		if c.land {
			landCount++
			lands = append(lands, c)
			continue
		}
		// A spell is castable by turn 3 with the hand's lands.
		if has && mullCardCastable(cv, hand) {
			castable = append(castable, c)
		} else {
			uncastable = append(uncastable, c)
		}
	}
	order := mullBottomOrder(landCount, lands, uncastable, castable)
	if len(order) < d.Min {
		return decision.Intent{}, false
	}
	picks := make([]int, 0, d.Min)
	for _, c := range order[:d.Min] {
		picks = append(picks, c.idx)
	}
	return decision.Intent{Choices: picks}, true
}

// mullCandidate is one bottoming option with the facts the order reads.
type mullCandidate struct {
	idx  int
	land bool
	cmc  int32
}

// mullBottomOrder returns the bottoming candidates in bottoming priority
// order: excess lands, then uncastable spells by mana value descending, then
// castable spells by mana value descending, then the remaining lands. Ties
// break on the option index, so the order is total and deterministic.
func mullBottomOrder(landCount int, lands, uncastable, castable []mullCandidate) []mullCandidate {
	used := make(map[int]bool, len(lands)+len(uncastable)+len(castable)) // lookup only
	var order []mullCandidate
	// 1. Lands in excess of four, so the kept hand keeps 2..4 lands. Which
	// lands go is arbitrary among equals; the highest option index first is
	// as good as any and is deterministic.
	if landCount > 4 {
		ls := append([]mullCandidate(nil), lands...)
		sort.Slice(ls, func(i, j int) bool { return ls[i].idx > ls[j].idx })
		for k := 0; k < landCount-4 && k < len(ls); k++ {
			order = append(order, ls[k])
			used[ls[k].idx] = true
		}
	}
	sortByCost(uncastable)
	sortByCost(castable)
	order = append(order, uncastable...)
	order = append(order, castable...)
	// 4. Remaining lands, only if the picks above did not fill the quota.
	for _, c := range lands {
		if !used[c.idx] {
			order = append(order, c)
		}
	}
	return order
}

// sortByCost orders candidates by mana value descending, option index
// ascending -- a total order.
func sortByCost(cs []mullCandidate) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].cmc != cs[j].cmc {
			return cs[i].cmc > cs[j].cmc
		}
		return cs[i].idx < cs[j].idx
	})
}

// mullHand returns the deciding seat's own hand from the view. ok is false
// when the view carries no such seat or its Hand is nil (not the viewer's
// own -- a redacted hidden zone).
func mullHand(v view.View, p state.PlayerID) ([]view.CardView, bool) {
	for i := range v.Players {
		if v.Players[i].ID == p {
			if v.Players[i].Hand == nil {
				return nil, false
			}
			return v.Players[i].Hand, true
		}
	}
	return nil, false
}

// mullSplit splits a hand into its land and non-land cards, preserving hand
// order.
func mullSplit(hand []view.CardView) (lands, spells []view.CardView) {
	for _, cv := range hand {
		if mullIsLand(cv) {
			lands = append(lands, cv)
		} else {
			spells = append(spells, cv)
		}
	}
	return lands, spells
}

// mullIsLand reports whether the card's joined type list contains the Land
// type word.
func mullIsLand(cv view.CardView) bool {
	for _, t := range strings.Fields(cv.Types) {
		if strings.EqualFold(t, "Land") {
			return true
		}
	}
	return false
}

// mullSources accumulates, per colour (W U B R G), how many of lands can
// produce it. An "add any colour" source counts as one source of every
// colour. A non-land, or a land with no mana ability, contributes nothing.
func mullSources(lands []view.CardView) (sources [5]int) {
	for _, cv := range lands {
		if cv.Produces == nil {
			continue
		}
		if cv.Produces.Any {
			for c := 0; c < 5; c++ {
				sources[c]++
			}
			continue
		}
		for c := 0; c < 5; c++ {
			if cv.Produces.Colour[c] > 0 {
				sources[c]++
			}
		}
	}
	return sources
}

// mullHasTurn3Castable reports whether any of spells is castable by turn 3
// with the hand's lands.
func mullHasTurn3Castable(spells, lands []view.CardView) bool {
	for _, cv := range spells {
		if mullCardCastable(cv, lands) {
			return true
		}
	}
	return false
}

// mullCardCastable reports whether cv can be cast by turn 3 with lands: its
// mana value is at most min(len(lands), 3), each coloured pip it needs has a
// producing land, and it needs no more coloured pips than lands.
func mullCardCastable(cv view.CardView, lands []view.CardView) bool {
	available := len(lands)
	if available > 3 {
		available = 3
	}
	if available == 0 {
		return false
	}
	cmc := botpolicy.CmcOf(cv.ManaCost)
	if cmc > int32(available) {
		return false
	}
	pips, coloured := mullPips(cv.ManaCost)
	if coloured > available {
		return false
	}
	sources := mullSources(lands)
	for c := 0; c < 5; c++ {
		n := sources[c]
		if n > available {
			n = available
		}
		if pips[c] > n {
			return false
		}
	}
	return true
}

// mullLandsProduceNeededColour reports whether the lands produce at least
// one colour the hand's spells need.
func mullLandsProduceNeededColour(spells, lands []view.CardView) bool {
	var needed [5]bool
	for _, cv := range spells {
		pips, _ := mullPips(cv.ManaCost)
		for c := 0; c < 5; c++ {
			if pips[c] > 0 {
				needed[c] = true
			}
		}
	}
	for _, cv := range lands {
		if cv.Produces == nil {
			continue
		}
		if cv.Produces.Any {
			for c := 0; c < 5; c++ {
				if needed[c] {
					return true
				}
			}
			continue
		}
		for c := 0; c < 5; c++ {
			if needed[c] && cv.Produces.Colour[c] > 0 {
				return true
			}
		}
	}
	return false
}

// mullPips parses a Forge-notation mana cost into its per-colour pip counts
// (W U B R G) and the total coloured pip count. Braces are stripped; a
// single-colour symbol is one pip of that colour; any other symbol naming
// colour letters (a hybrid or Phyrexian pip) is counted as one pip of EACH
// colour it names -- deliberately harder to satisfy than the rules, the safe
// direction for a keep rule.
func mullPips(mc string) (pips [5]int, coloured int) {
	mc = strings.NewReplacer("{", " ", "}", " ").Replace(mc)
	for _, sym := range strings.Fields(mc) {
		for k := 0; k < len(sym); k++ {
			if c := mullColourIndex(sym[k]); c >= 0 {
				pips[c]++
				coloured++
			}
		}
	}
	return pips, coloured
}

// mullColourIndex maps a mana-symbol byte to its colour slot (0 W, 1 U, 2 B,
// 3 R, 4 G), or -1 for anything else.
func mullColourIndex(b byte) int {
	switch b {
	case 'W', 'w':
		return 0
	case 'U', 'u':
		return 1
	case 'B', 'b':
		return 2
	case 'R', 'r':
		return 3
	case 'G', 'g':
		return 4
	}
	return -1
}

// mullOptionKind finds the option index whose Kind is kind.
func mullOptionKind(d decision.Decision, kind string) (int, bool) {
	for _, o := range d.Options {
		if o.Kind == kind {
			return o.Index, true
		}
	}
	return 0, false
}
