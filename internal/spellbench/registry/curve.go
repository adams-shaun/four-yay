package registry

// curve is a SpellBench idea decorator: on the wrapped seat's own
// main-phase priority decisions with an empty stack it spends the turn's
// resources in a fixed order, and it delegates everything else untouched.
//
//  1. Land first. If the decision offers a land play, take the one whose
//     production unlocks the most castable hand spells this turn and next.
//  2. Spend the mana. Among the offered sorcery-speed "cast" options, pick
//     the subset of hand spells that maximises the total mana value the
//     untapped sources can pay (a small knapsack over mana value and colour
//     requirements) and cast the highest-mana-value spell of that subset.
//  3. Instants and flash cards are never cast by this decorator, so the
//     wrapped seat's hold-up logic still owns them.
//  4. Any other decision -- a different kind, a non-main phase, a non-empty
//     stack, an opponent's main phase, no land play and no sorcery-speed
//     spell the sources can pay -- delegates to the wrapped seat unchanged.
//
// The wrapper is modelled on passguard (curve.go sits beside it in this
// package): it keeps the wrapped seat's BoardSeat-ness and payment opt-in,
// exposes the wrapped seat through UnwrapperSeat, and consults the wrapped
// seat exactly once per decision so a seed-streamed policy draws the same
// numbers it would have drawn bare.
//
// Information surface. Everything the idea reads is information the deciding
// seat can see: its own hand and command zone, the public battlefield, the
// decision's own options. The view half reads view.CardView (ManaCost,
// Types/Keywords, Produces, Tapped); the board half reads the same facts
// from botpolicy.Board.Cards (ManaCost, InstantSpeed, Produces, Tapped,
// OnBattlefield). Card characteristics come from gorge's card IR, surfaced
// by view/botpolicy; the decorator never reads an opponent's hidden zones.
//
// Determinism. Every argmax is a total order over the decision's options
// with an explicit index tie-break, the source aggregate is order-free, and
// the seed argument is unused (the idea has no randomness). No map range
// reaches a choice.

import (
	"context"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/seat"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func init() {
	RegisterDecorator("curve", newCurve)
}

// curveKnapsackCap bounds the subset enumeration: a hand of more than this
// many sorcery-speed cast options takes the first options in the decision's
// own (deterministic) order, so the 2^n walk stays small. A real hand is
// far below it.
const curveKnapsackCap = 16

// newCurve wraps inner. Like passguard, the wrapper keeps inner's
// BoardSeat-ness so the engine still hands a BoardSeat inner its board.
func newCurve(inner seat.Seat, _ uint64) seat.Seat {
	base := curveSeat{inner: inner}
	if _, ok := inner.(seat.BoardSeat); ok {
		return curveBoard{base}
	}
	return base
}

// curveSeat is the plain wrapper over a non-BoardSeat inner.
type curveSeat struct {
	inner seat.Seat
}

// UnwrapSeat exposes the wrapped seat (the registry's Unwrapper contract).
func (s curveSeat) UnwrapSeat() seat.Seat { return s.inner }

func (s curveSeat) Decide(ctx context.Context, v view.View, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.Decide(ctx, v, d)
	if err != nil {
		return in, err
	}
	if out, ok := curveGuard(d, curveFactsFromView(v.Viewer, v)); ok {
		return out, nil
	}
	return in, nil
}

// WantsPaymentActions delegates the payment-plan opt-in: the wrapper offers
// exactly what inner offers.
func (s curveSeat) WantsPaymentActions() bool {
	if c, ok := s.inner.(seat.PaymentPlanConsumer); ok {
		return c.WantsPaymentActions()
	}
	return false
}

// curveBoard is the wrapper over a BoardSeat inner.
type curveBoard struct {
	curveSeat
}

func (s curveBoard) DecideBoard(ctx context.Context, b botpolicy.Board, d decision.Decision) (decision.Intent, error) {
	in, err := s.inner.(seat.BoardSeat).DecideBoard(ctx, b, d)
	if err != nil {
		return in, err
	}
	if out, ok := curveGuard(d, curveFactsFromBoard(b)); ok {
		return out, nil
	}
	return in, nil
}

// curveSpell is one card the deciding seat can see: a spell (for the
// knapsack and the land ranking) or a land card in hand (for the land
// ranking).
type curveSpell struct {
	manaCost string
	cmc      int32
	instant  bool
	land     bool
	produces cards.ManaProduction
}

// curveSource is one battlefield permanent that can produce mana.
type curveSource struct {
	produces cards.ManaProduction
}

// curveFacts is the surface-normalised board the guard reads. main/stackEmpty/
// ourTurn gate the idea; byObj resolves an offered option's card; spells are
// the deciding seat's own non-land castable cards; untapped/all are the
// battlefield mana sources before and after next turn's untap.
type curveFacts struct {
	main       bool
	stackEmpty bool
	ourTurn    bool
	byObj      map[state.ObjID]curveSpell
	spells     []curveSpell
	untapped   []curveSource
	all        []curveSource
}

// curveFactsFromView builds the facts from the deciding seat's projected
// view.
func curveFactsFromView(me state.PlayerID, v view.View) curveFacts {
	f := curveFacts{
		main:       v.Phase == "main1" || v.Phase == "main2",
		stackEmpty: len(v.Stack) == 0,
		ourTurn:    v.Active == me,
		byObj:      map[state.ObjID]curveSpell{},
	}
	for i := range v.Players {
		p := &v.Players[i]
		if p.ID != me {
			continue
		}
		for j := range p.Hand {
			cv := &p.Hand[j]
			cs := curveSpellFromView(cv)
			f.byObj[cv.ID] = cs
			if !cs.land {
				f.spells = append(f.spells, cs)
			}
		}
		for j := range p.Battlefield {
			cv := &p.Battlefield[j]
			if cv.Produces == nil || cv.Produces.IsZero() {
				continue
			}
			src := curveSource{produces: *cv.Produces}
			f.all = append(f.all, src)
			if !cv.Tapped {
				f.untapped = append(f.untapped, src)
			}
		}
	}
	return f
}

// curveSpellFromView projects one visible card's facts.
func curveSpellFromView(cv *view.CardView) curveSpell {
	cs := curveSpell{
		manaCost: cv.ManaCost,
		cmc:      botpolicy.CmcOf(cv.ManaCost),
		instant:  curveInstant(cv.Types, cv.Keywords),
		land:     strings.Contains(cv.Types, "Land"),
	}
	if cv.Produces != nil {
		cs.produces = *cv.Produces
	}
	return cs
}

// curveInstant reports whether a card is castable at instant speed: an
// Instant, or a card carrying the Flash keyword.
func curveInstant(types string, keywords []string) bool {
	if strings.Contains(types, "Instant") {
		return true
	}
	for _, k := range keywords {
		if k == "Flash" {
			return true
		}
	}
	return false
}

// curveFactsFromBoard builds the facts from the deciding seat's board. The
// board does not distinguish a hand card from a flashback graveyard card
// (both are Castable and off the battlefield), so the land ranking's spell
// census counts every castable off-battlefield card. The claimed cases (the
// land play and the offered cast) resolve by object id and are exact.
func curveFactsFromBoard(b botpolicy.Board) curveFacts {
	f := curveFacts{
		main:       b.IsMain,
		stackEmpty: len(b.Stack) == 0,
		ourTurn:    b.MyTurn,
		byObj:      map[state.ObjID]curveSpell{},
	}
	for id, c := range b.Cards {
		if c.OnBattlefield {
			if c.Produces.IsZero() {
				continue
			}
			src := curveSource{produces: c.Produces}
			f.all = append(f.all, src)
			if !c.Tapped {
				f.untapped = append(f.untapped, src)
			}
			continue
		}
		cs := curveSpell{
			manaCost: c.ManaCost,
			cmc:      c.CMC,
			instant:  c.InstantSpeed,
			produces: c.Produces,
		}
		f.byObj[id] = cs
		// Castable already excludes a land (a land is never cast); the
		// board carries no type line, so a hand mana creature or rock stays
		// in the spell census rather than being misread as a land.
		if c.Castable {
			f.spells = append(f.spells, cs)
		}
	}
	return f
}

// curveGuard applies the idea. It returns ok=false when the decision is not
// one of the idea's own or when the idea has no play, so the caller returns
// the wrapped seat's answer unchanged.
func curveGuard(d decision.Decision, f curveFacts) (decision.Intent, bool) {
	if d.Kind != decision.KPriority || !f.main || !f.stackEmpty || !f.ourTurn {
		return decision.Intent{}, false
	}
	if idx, ok := f.bestLand(d); ok {
		return curveIntent(d, idx), true
	}
	if idx, ok := f.bestCast(d); ok {
		return curveIntent(d, idx), true
	}
	return decision.Intent{}, false
}

// curveIntent is a plain single-choice answer for d. It carries no payment
// witness or announce: the option the idea picks is an ordinary offered
// play, which the engine pays for exactly as the wrapped seat's own plain
// answer would.
func curveIntent(d decision.Decision, idx int) decision.Intent {
	return decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}
}

// bestLand is step 1: the offered land play that unlocks the most castable
// hand spells, with the lowest option index breaking ties.
func (f curveFacts) bestLand(d decision.Decision) (int, bool) {
	now := curveAvailabilityOf(f.untapped)
	next := curveAvailabilityOf(f.all)
	best, bestScore := -1, -1
	for _, o := range d.Options {
		if o.Kind != "play_land" {
			continue
		}
		sp, ok := f.byObj[o.Obj]
		if !ok || sp.produces.IsZero() {
			continue
		}
		score := f.landScore(now, next, sp.produces)
		if best < 0 || score > bestScore {
			best, bestScore = o.Index, score
		}
	}
	return best, best >= 0
}

// landScore is how many castable hand spells a land unlocks: the count whose
// cost becomes payable with the land added to the currently untapped sources
// (this turn) plus the count payable with it added to every battlefield
// source (next turn, after the untap).
func (f curveFacts) landScore(now, next curveAvailability, p cards.ManaProduction) int {
	now.add(p)
	next.add(p)
	return f.countCastable(now) + f.countCastable(next)
}

// countCastable counts the deciding seat's own non-land spells the given
// availability can pay.
func (f curveFacts) countCastable(a curveAvailability) int {
	n := 0
	for i := range f.spells {
		if a.pay(curveParseDemand(f.spells[i].manaCost)) {
			n++
		}
	}
	return n
}

// bestCast is step 2: the subset of castable sorcery-speed spells that
// maximises total mana value, then the highest single mana value, then the
// lowest first option index; the returned option is the highest-mana-value
// spell of that subset. It returns ok=false when the best subset spends no
// mana (an empty hand of payable sorceries), so the caller delegates.
func (f curveFacts) bestCast(d decision.Decision) (int, bool) {
	type cand struct {
		idx int
		dem curveDemand
		cmc int32
	}
	var cands []cand
	for _, o := range d.Options {
		if o.Kind != "cast" {
			continue
		}
		sp, ok := f.byObj[o.Obj]
		if !ok || sp.instant {
			continue
		}
		cands = append(cands, cand{idx: o.Index, dem: curveParseDemand(sp.manaCost), cmc: sp.cmc})
	}
	if len(cands) == 0 {
		return -1, false
	}
	if len(cands) > curveKnapsackCap {
		cands = cands[:curveKnapsackCap]
	}
	avail := curveAvailabilityOf(f.untapped)
	n := len(cands)
	var bestTotal, bestMax int32 = 0, 0
	bestFirst, bestMask := 1<<30, 0
	for mask := 1; mask < 1<<n; mask++ {
		var pips [6]int32
		var sources, total int32
		for i := 0; i < n; i++ {
			if mask&(1<<i) == 0 {
				continue
			}
			c := cands[i]
			for col := 0; col < 6; col++ {
				pips[col] += c.dem.pips[col]
			}
			sources += c.dem.sources
			total += c.cmc
		}
		if total == 0 || sources > avail.have {
			continue
		}
		payable := true
		for col := 0; col < 6; col++ {
			if pips[col] > avail.colour[col] {
				payable = false
				break
			}
		}
		if !payable {
			continue
		}
		// The spell of this subset "cast first": its highest mana value,
		// lowest option index.
		var top int32 = -1
		fi := 1 << 30
		for i := 0; i < n; i++ {
			if mask&(1<<i) == 0 {
				continue
			}
			c := cands[i]
			if c.cmc > top || (c.cmc == top && c.idx < fi) {
				top, fi = c.cmc, c.idx
			}
		}
		if total > bestTotal ||
			(total == bestTotal && top > bestMax) ||
			(total == bestTotal && top == bestMax && fi < bestFirst) {
			bestTotal, bestMax, bestFirst, bestMask = total, top, fi, mask
		}
	}
	if bestMask == 0 {
		return -1, false
	}
	// The option to cast: the subset's highest-mana-value member, lowest
	// option index (the tie-break the subset selection already keyed on).
	idx := -1
	var top int32 = -1
	for i := 0; i < n; i++ {
		if bestMask&(1<<i) == 0 {
			continue
		}
		c := cands[i]
		if c.cmc > top || (c.cmc == top && (idx < 0 || c.idx < idx)) {
			top, idx = c.cmc, c.idx
		}
	}
	if idx < 0 {
		return -1, false
	}
	return idx, true
}

// curveDemand is a parsed mana cost: the exact coloured pips, the total
// source units required (generic + pips + any hybrid/phyrexian symbol), and
// the mana value. The value is botpolicy.CmcOf's, so it agrees with every
// other CMC reader in the pipeline.
type curveDemand struct {
	pips    [6]int32
	sources int32
	cmc     int32
}

// curveParseDemand parses a Forge mana cost ("2 B B", "{1}{U}{U}", "X G").
// A literal counts as generic units, a single colour letter as a pip, and
// anything else (hybrid, phyrexian) as one any-source unit -- the same
// conservative model rules/mana.go's own conservative affordability check
// (builtins' canAfford) uses, so the decorator and the wrapped seat agree
// about what "payable" means. {X} contributes nothing.
func curveParseDemand(cost string) curveDemand {
	var d curveDemand
	d.cmc = botpolicy.CmcOf(cost)
	norm := strings.NewReplacer("{", " ", "}", " ").Replace(cost)
	for _, fld := range strings.Fields(norm) {
		if n, err := strconv.Atoi(fld); err == nil {
			if n > 0 {
				d.sources += int32(n)
			}
			continue
		}
		if fld == "X" || fld == "x" {
			continue
		}
		if len(fld) == 1 {
			if i := strings.IndexByte("WUBRGC", fld[0]); i >= 0 {
				d.pips[i]++
				d.sources++
				continue
			}
		}
		d.sources++ // hybrid, phyrexian, or any other symbol
	}
	return d
}

// curveAvailability is the aggregate mana the sources can produce: the
// number of source units, plus a per-colour count. A source producing
// several colours contributes one unit to each of them, exactly as
// builtins' canAfford counts it.
type curveAvailability struct {
	have   int32
	colour [6]int32
}

func curveAvailabilityOf(srcs []curveSource) curveAvailability {
	var a curveAvailability
	for i := range srcs {
		a.add(srcs[i].produces)
	}
	return a
}

// add folds one production into the aggregate. A zero production adds
// nothing.
func (a *curveAvailability) add(p cards.ManaProduction) {
	if p.IsZero() {
		return
	}
	a.have++
	listed := p.Colour != [6]int32{}
	for c := 0; c < 6; c++ {
		if p.Colour[c] > 0 || (p.Any && !listed) {
			a.colour[c]++
		}
	}
}

// pay reports whether the aggregate can pay the demand: enough source units
// in total and enough of each demanded colour.
func (a curveAvailability) pay(d curveDemand) bool {
	if d.sources > a.have {
		return false
	}
	for c := 0; c < 6; c++ {
		if d.pips[c] > a.colour[c] {
			return false
		}
	}
	return true
}
