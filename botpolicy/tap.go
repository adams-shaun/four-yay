package botpolicy

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// T1 -- the need-aware tap gate (task op6). The tap-for-mana "activate"
// options legalActions (rules/legal.go) offers are gated on "do any of the
// cards this seat could cast need mana the pool does not yet have". The old
// position-first block tapped the first "activate" in every main phase
// until every source was spent, floating mana in bulk and letting each
// step's end (CR 500.4) empty whatever a cast never spent; this file
// replaces the WHEN with a need check computed from board facts alone (the
// policy is stateless: no memory across decisions).
//
// The WHICH was position-first when op6 landed, because the View then
// projected no per-permanent mana production and nothing in the offer
// distinguished a Plains from an Island. It no longer is: CardView.Produces
// (dp2) carries each permanent's produced colours, so chooseTap below picks
// the source whose colour answers a pip the pool still owes. See chooseTap's
// own doc comment for the ordering rules; what survives from op6 here is the
// WHEN, not the WHICH.
//
// The check is deliberately the engine's own gate re-derived at the
// granularity botpolicy may read: a card is "castable with the pool" when
// the pool's total covers its converted cost and its coloured pips cover
// every coloured symbol in its printed cost -- the CanPay shape
// rules/cast.go's castable runs, re-expressed over the Board's Card facts
// because botpolicy cannot import rules (Ruling F7). The engine only ever
// offers a "cast" option once the CURRENT pool can pay the cost, so
// "nothing castable yet + tapping could help" is exactly "some castable
// card the pool cannot pay" -- the triggers of the tap loop, and the
// moment the pool pays the cheapest such card the cast options open and
// chooseCast takes over.

// hasFlashback is the tap gate's graveyard-castability test: does the
// derived keyword list (the engine's Derived() output, which the View also
// projects as CardView.Keywords) carry the Flashback keyword? It is the
// policy's mirror of rules/legal.go's own flashback-gate read (e.HasKeyword
// over the same list), with the same head-strip cards.KeywordHead applies
// so a parameterised keyword can never shadow the head word.
func hasFlashback(kws []string) bool {
	for _, k := range kws {
		if strings.EqualFold(cards.KeywordHead(k), "Flashback") {
			return true
		}
	}
	return false
}

// hasFlash is the instant-speed half of the mana reserve: does the derived
// keyword list (the engine's Derived() output, which the View also projects
// as CardView.Keywords) carry the Flash keyword -- a permanent with flash
// enters at instant speed, so it is playable on another seat's turn. It is
// the same head-strip cards.KeywordHead applies everywhere, mirroring
// rules/legal.go's own Flash speed gate.
func hasFlash(kws []string) bool {
	for _, k := range kws {
		if strings.EqualFold(cards.KeywordHead(k), "Flash") {
			return true
		}
	}
	return false
}

// tapWants reports whether any card the deciding seat could cast from a
// castable zone (Card.Castable: hand, command zone, graveyard with
// Flashback) has a cost the current pool cannot pay:
//
//   - cost is the printed converted mana cost, plus the CR 903.8 command
//     tax ({2} per prior command-zone cast) when the card is its owner's
//     commander sitting in the command zone -- the same tax chooseCast's
//     CR1 prices, so a tapped-out commander cast is not gated early;
//   - a card with CMC 0 (a land, a free spell) never wants a tap: it is
//     castable without mana or played through the land drop;
//   - the pool covers the card when its total is at least the cost AND
//     each coloured pip of the printed cost has a matching pool slot (the
//     colour-blindness of CmcOf stops here: a {U}{B} card against a
//     {U}{U} pool still wants a tap, because the pool cannot pay it).
//
// A card the seat cannot currently cast for ANY amount of mana for a
// non-mana reason (no legal targets, a timing restriction) still triggers
// taps: the policy cannot read those facts, and its old behaviour did
// worse (it tapped regardless of the hand at all).
func (b Board) tapWants() bool {
	for id, c := range b.Cards {
		if !c.Castable || c.CMC <= 0 {
			continue
		}
		if !b.poolPays(id, c) {
			return true
		}
	}
	return false
}

// poolPays prices one castable card against the current pool: can the pool
// pay its cost (CMC plus the command-zone tax, CR 903.8) with its coloured
// pips covered? This is the policy's read-only approximation of the
// engine's CanPay, at the CmcOf granularity the Board carries.
func (b Board) poolPays(id state.ObjID, c Card) bool {
	cost := c.CMC
	if cmdr, ok := b.Commanders[id]; ok && cmdr.InCommandZone {
		cost += 2 * cmdr.Casts
	}
	if b.Pool.Total() < cost {
		return false
	}
	// The coloured-pip demands: a single-letter W/U/B/R/G symbol is one
	// pip of that colour; every other symbol (numbers, {X}, hybrids,
	// Phyrexian, colourless) is generic, which CmcOf already counted in
	// CMC and any mana pays. Pips are counted per colour first, then
	// compared with the pool once, so a {U}{U} card against a single
	// floating blue still wants a tap.
	pips := colourPips(c.ManaCost)
	for si, n := range pips {
		if n > b.Pool[si] {
			return false
		}
	}
	return true
}

// colourPips counts the coloured (WUBRG) pips of a printed Forge mana cost
// into the five colour slots. A single-letter W/U/B/R/G symbol is one pip of
// that colour; every other symbol (numbers, {X}, hybrids, Phyrexian,
// colourless) is generic -- CmcOf already counted it in CMC and any mana
// pays it. This is the shared pip count behind both poolPays and the
// colour-aware tap gate's need map, so the two always agree.
func colourPips(mc string) [5]int32 {
	var pips [5]int32
	for sym := range strings.FieldsSeq(braceForm.Replace(mc)) {
		if len(sym) != 1 {
			continue
		}
		si := state.ManaIndex(sym[0]) // MW..MG for WUBRG, MC for anything else
		if si > state.MG {
			continue // not one of the five colours
		}
		pips[si]++
	}
	return pips
}

// cardScore ranks one card the way chooseCast ranks an offered cast (cast.go's
// castScore), so the tap gate can pick the same "intended" card the casting
// policy would most want to enable: a creature scores 30 + 4*Power, a
// non-creature its mana value. The tap gate reads it to choose WHICH card's
// colour need to tap toward, not which card to cast -- the cast itself stays
// with chooseCast.
func (b Board) cardScore(c Card) int32 {
	if c.Creature {
		return 30 + c.Power*4
	}
	return c.CMC
}

// bestUnpayable returns the castable card the pool cannot currently pay that
// the casting policy would most want to enable (highest cardScore). It is the
// tap gate's "intended spell": the card that makes tapWants true, whose
// colour need chooseTap targets. It consumes no rng and breaks ties on object
// id, so map iteration order cannot reach the pick.
//
// offered (the satisfiability filter) keeps a card as a tap target only when
// THIS window's offered "activate" sources can actually close its gap: every
// unmet coloured pip must be producible by some offered source. An unmet pip
// no offered source can produce is a gap tapping can never close, and tapping
// a generic source toward it floats mana the card still cannot spend -- the
// measured seed-1003 ulalek-eldrazi livelock, where Ugin, Eye of the Storms'
// repeatable [0] ({C}{C}{C}, a loyalty cost) was re-tapped once per intent
// toward a green card's pip while the turn never advanced. A purely generic
// shortfall always qualifies: a tier-1 tap adds to the pool total and the
// progress is finite. A KNOWN empty production stays a non-producer (the bl1
// honesty contract); an entry the Board carries no facts for claims NOTHING
// (fail closed). The earlier fail-open read -- "unknown production, not
// absent production" -- was written for the synthetic policy-test shapes and
// presumed a real adapter never offers an activate option the Board lacks
// facts for. That premise is false: a battlefield copy (Echoes of Eternity's
// copy of a Dreamstone Hedron) is a real offer with no facts, and the
// fail-open let the seed-1019 livelock's green-card tap target back in. (The
// older parenthetical here cited state.Object.Ephemeral's IsCopy half as
// keeping such a copy out of the adapters' Cards tables; that is no longer
// true -- token1 clears IsCopy when a stack copy resolves onto the
// battlefield, and Ephemeral is zone-aware, so it hides NO battlefield
// object. The fail-closed reading is independent of Ephemeral and stays.)
// Fail-closed errs toward passing: the gate never taps a source it cannot
// price, the window falls through to the land drop and the cast, and the
// game advances.
func (b Board) bestUnpayable(offered [5]bool) (state.ObjID, Card, bool) {
	bestID := state.ObjID(0)
	var best Card
	var bestScore int32 = -1
	found := false
	for id, c := range b.Cards {
		if !c.Castable || c.CMC <= 0 {
			continue
		}
		if b.poolPays(id, c) {
			continue
		}
		// The satisfiability filter: an unmet coloured pip the offered
		// sources cannot produce excludes the card -- tapping toward it can
		// never make it payable.
		pips := colourPips(c.ManaCost)
		closable := true
		for i := 0; i < 5; i++ {
			if pips[i] > b.Pool[i] && !offered[i] {
				closable = false
				break
			}
		}
		if !closable {
			continue
		}
		s := b.cardScore(c)
		if !found || s > bestScore || (s == bestScore && int(id) < int(bestID)) {
			bestID, best, bestScore, found = id, c, s, true
		}
	}
	return bestID, best, found
}

// neededColours is the set of coloured pips the pool cannot yet cover for one
// card: index i is true when the card's cost carries more pips of colour i
// than the pool holds. This is exactly the colour the tap gate should aim a
// tap at -- a source that yields an already-covered colour adds nothing toward
// making this card castable.
func (b Board) neededColours(c Card) [5]bool {
	pips := colourPips(c.ManaCost)
	var need [5]bool
	for i := 0; i < 5; i++ {
		need[i] = pips[i] > b.Pool[i]
	}
	return need
}

// chooseTap is the KPriority tap gate. It returns one "activate" option when
// the pool cannot yet pay any castable card, or -1 when tapping has nothing to
// enable -- in which case the caller falls through to the land drop and the
// cast.
//
// T2 (Task dp2): the WHICH source is colour-aware instead of position-first.
// chooseTap names the intended card (the best-scoring castable card the pool
// cannot pay) and taps toward its colour need, ordered by a three-tier
// preference that the production honesty fix (bl1) tightened:
//
//   - tier 0: a source that DEMONSTRABLY produces a colour the intended card
//     needs (producesColour, which is only true for a known, guaranteed
//     colour slot -- an indeterminate source contributes nothing to claim
//     it), OR a REFLECTED source (Produces.Reflected), whose colour is
//     computed at resolution from what other objects produce and resolves to
//     whatever colour is asked for when it is tapped. This is the only tier a
//     tap is aimed at a specific pip. A plain conditional "Any" source
//     (Cavern of Souls, and 500+ other corpus cards) is deliberately NOT
//     here: widening the gate to every Any source is a separate behaviour
//     change from the reflected-source fix and moves the botbench golden
//     (ticket cli-20260922T225137Z), so only Produces.Reflected qualifies.
//   - tier 1: a source that demonstrably produces some mana, but none of a
//     needed colour (a screw in colour, or a pure generic shortfall): it at
//     least adds to the pool, so it outranks a source that may produce
//     nothing at all.
//   - tier 2: a source that demonstrably produces nothing -- no known
//     colour slot. An Indeterminate-amount source lands here because it
//     contributes zero to every colour slot (detected via Colour, never by
//     reading the Indeterminate flag), so such a source is a last resort
//     only: it might produce nothing, so chooseTap never PREFERS it over a
//     source that demonstrably produces.
//   - within a tier, the source with the FEWEST distinct colours is tapped
//     first (Task dp2's least-flexible-first: the mono-colour source before
//     the dual, keeping the flexible source for the colour it is still
//     needed for); a tie breaks on option index.
//
// It consumes no rng: the pick is a pure function of the offered options and
// the board facts.
func (b Board) chooseTap(d *decision.Decision) int {
	if !b.tapWants() {
		return -1
	}
	_, c, ok := b.bestUnpayable(b.offeredColours(d))
	if !ok {
		return -1
	}
	castOffered := false
	for _, o := range d.Options {
		if o.Kind == "cast" {
			castOffered = true
			break
		}
	}
	need := b.neededColours(c)
	best := -1
	bestTier := 3
	bestFlex := 0
	for _, o := range d.Options {
		if o.Kind != "activate" {
			continue
		}
		if conv, spend := converterCost(o.Cost); conv {
			// T3: a converter (see converterCost) is taken only when it
			// provably moves the intended card closer to castable.
			if castOffered || !b.conversionProgresses(c, spend, b.Cards[o.Obj].Produces) {
				continue
			}
		}
		prod := b.Cards[o.Obj].Produces
		matches := false
		for i := 0; i < 5; i++ {
			if need[i] && (prod.ProducesColour(i) || prod.Reflected || prod.Any) {
				matches = true
				break
			}
		}
		// demonstrable: a colour slot > 0 is production the pool will
		// actually receive (an indeterminate amount contributes 0 to every
		// slot, so it cannot make this true).
		demonstrable := prod.Colour[0] > 0 || prod.Colour[1] > 0 || prod.Colour[2] > 0 ||
			prod.Colour[3] > 0 || prod.Colour[4] > 0 || prod.Colour[5] > 0
		tier := 2
		if matches {
			tier = 0
		} else if demonstrable {
			tier = 1
		}
		flex := prod.DistinctColours()
		if best == -1 || tier < bestTier ||
			(tier == bestTier && flex < bestFlex) ||
			(tier == bestTier && flex == bestFlex && o.Index < best) {
			best, bestTier, bestFlex = o.Index, tier, flex
		}
	}
	return best
}

// offeredColours is the union of what THIS window's offered "activate"
// sources demonstrably produce: the satisfiability filter bestUnpayable
// reads (see its doc).
func (b Board) offeredColours(d *decision.Decision) [5]bool {
	var offered [5]bool
	for _, o := range d.Options {
		if o.Kind != "activate" {
			continue
		}
		card, known := b.Cards[o.Obj]
		if !known {
			// Fail closed: an option the Board carries no facts for claims
			// no colour (see bestUnpayable's doc for why the earlier
			// fail-open read was wrong). chooseTap's pick loop already
			// prices such an option as a tier-2 last resort, so the two
			// reads agree.
			continue
		}
		for i := 0; i < 5; i++ {
			if card.Produces.Colour[i] > 0 || card.Produces.Reflected || card.Produces.Any {
				offered[i] = true
			}
		}
	}
	return offered
}

// TapIntent names the card the KPriority tap gate (chooseTap) is tapping
// toward -- its "intended spell", bestUnpayable over this window's offered
// sources -- or ok=false when the gate would not tap at all. It is a pure
// read of the same facts chooseTap reads and consumes no rng. The auto-pay
// adapter (seat/bot.go) reads it to notice when the gate's intended card is
// a cast the V1 planner can offer no plan for, and answers with the manual
// policy instead so the card can still be paid.
func (b Board) TapIntent(d *decision.Decision) (state.ObjID, bool) {
	if !b.tapWants() {
		return 0, false
	}
	id, _, ok := b.bestUnpayable(b.offeredColours(d))
	return id, ok
}

// T3 -- the converter gate (cardfuzz batch1 lines 3/5/6/10/16/17). A mana
// ability whose cost spends POOL mana and does not tap its source (Farrelite
// Priest's and Bog Initiate's "{1}: Add {W}/{B}", Initiates of the Ebon
// Hand) is a converter: it can be activated any number of times, and each
// activation takes mana out of the very pool the tap gate is trying to fill.
// The T1/T2 gate prices a source only by what it PRODUCES, so a converter
// that produces a needed colour ranked tier 0 and was re-activated forever:
// the engine pays its generic {1} out of the pool in fixed C,W,U,B,R,G order
// (rules/mana.go resolveManaWith), which for a white card against a
// white-only pool spends the {W} it then adds back -- a net-zero cycle, one
// decision per loop, until the engine's livelock watcher fires.
//
// The gate is a class rule over the offered cost, not a card list:
//
//   - a converter is never activated while a "cast" option is offered: the
//     pool already pays something, and spending it on a conversion toward a
//     different card can only undo that (Bog Initiate converted the four {B}
//     that were paying for Frogmite);
//   - otherwise it is activated only when the hypothetical pool after paying
//     its mana cost and adding its production has a strictly smaller
//     payment deficit for the intended card (conversionDeficit) than the
//     current pool. The deficit is a non-negative integer that no tap and no
//     taken conversion ever raises for that card, so the conversions the
//     policy takes toward one intended card are finite, and every turn ends;
//   - a production the policy cannot price (an indeterminate amount) or a
//     cost it cannot read ({X}) is never activated -- fail closed toward the
//     pass, the gate's standing direction.
//
// A mana-costed ability that also TAPS its source (Celestial Prism's
// "{2}, {T}", a filter land) is bounded by the tap and is not a converter;
// it keeps the T2 ordering unchanged.

// converterCost parses an "activate" option's engine-supplied cost marker
// (decision.Option.Cost, rules/legal.go: Forge notation, empty for a bare
// {T}). It reports whether the cost spends pool mana WITHOUT tapping the
// source, and the mana it spends. Non-mana parts (Sac<...>, PayLife<...>,
// Return<...>) are ignored; an {X} marks the spend unknown, which
// conversionProgresses refuses.
func converterCost(cost string) (bool, manaSpend) {
	var sp manaSpend
	if strings.TrimSpace(cost) == "" {
		return false, sp
	}
	tapped, spends := false, false
	for _, tok := range strings.Fields(cost) {
		switch {
		case tok == "T":
			tapped = true
		case isDigits(tok):
			n := int32(0)
			for i := 0; i < len(tok); i++ {
				n = n*10 + int32(tok[i]-'0')
			}
			if n > 0 {
				sp.generic += n
				spends = true
			}
		case tok == "X":
			sp.unknown, spends = true, true
		case len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])):
			sp.pips[state.ManaIndex(tok[0])]++
			spends = true
		case strings.Contains(tok, "/") && !strings.Contains(tok, "<"):
			// A hybrid or Phyrexian pip (W/U, 2/W, G/P): priced as one
			// generic unit -- the policy cannot read which half the payer
			// uses, and a generic unit is the cheapest reading.
			sp.generic++
			spends = true
		}
	}
	return spends && !tapped, sp
}

// manaSpend is a converter's mana cost: generic units, per-slot coloured
// (and {C}) pips, and whether some part could not be priced.
type manaSpend struct {
	generic int32
	pips    state.Mana
	unknown bool
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// conversionProgresses is T3's progress test: would paying spend out of the
// current pool and adding the source's production leave the intended card c
// with a strictly smaller conversionDeficit? The payment is simulated the
// way the engine pays it (coloured pips from their slot, generic from C then
// W, U, B, R, G); a pool that cannot pay is no progress. A plain production
// adds exactly its Colour vector; a choice production (Any, Reflected) is
// priced as ONE unit of the card's most-needed colour (its guaranteed lower
// bound); an indeterminate amount cannot be priced and is no progress.
func (b Board) conversionProgresses(c Card, spend manaSpend, prod cards.ManaProduction) bool {
	if spend.unknown || prod.Indeterminate {
		return false
	}
	// Only the unrestricted pool (see Board.PoolRestricted) pays a
	// converter or the intended card in this simulation: a restricted unit
	// the engine would refuse must not stand in for the unit it will spend.
	base := b.Pool
	for i := range base {
		base[i] -= b.PoolRestricted[i]
		if base[i] < 0 {
			base[i] = 0
		}
	}
	pool := base
	for i := range pool {
		if pool[i] < spend.pips[i] {
			return false
		}
		pool[i] -= spend.pips[i]
	}
	g := spend.generic
	for _, i := range [...]int{state.MC, state.MW, state.MU, state.MB, state.MR, state.MG} {
		for g > 0 && pool[i] > 0 {
			pool[i]--
			g--
		}
	}
	if g > 0 {
		return false
	}
	pips := colourPips(c.ManaCost)
	if prod.Any || prod.Reflected {
		bestI, bestGap := int(state.MC), int32(0)
		for i := 0; i < 5; i++ {
			if gap := pips[i] - pool[i]; gap > bestGap {
				bestI, bestGap = i, gap
			}
		}
		pool[bestI]++
	} else {
		for i := range pool {
			pool[i] += prod.Colour[i]
		}
	}
	return conversionDeficit(c, pips, pool) < conversionDeficit(c, pips, base)
}

// RestrictedPool folds a seat's RestrictValid$ mana batches into per-slot
// counts (Board.PoolRestricted). A batch with an empty Valid (an
// AddsNoCounter$-only provenance batch) imposes no spend limit and is not
// counted -- the same batches view.PoolRestrictions omits, so the Board's
// game half and its view half agree.
func RestrictedPool(rs []state.ManaRestriction) state.Mana {
	var m state.Mana
	for _, r := range rs {
		if strings.TrimSpace(r.Valid) == "" || r.Amount <= 0 {
			continue
		}
		m[state.ManaSlot(r.Color)] += r.Amount
	}
	return m
}

// conversionDeficit is how far pool is from paying card c: the coloured pips
// it cannot cover plus the total mana it is short. Zero exactly when
// poolPays would accept (ignoring the command-zone tax, which only raises
// both sides equally). Adding mana never raises it.
func conversionDeficit(c Card, pips [5]int32, pool state.Mana) int32 {
	var d int32
	for i := 0; i < 5; i++ {
		if pips[i] > pool[i] {
			d += pips[i] - pool[i]
		}
	}
	if short := c.CMC - pool.Total(); short > 0 {
		d += short
	}
	return d
}

// T4 -- the payment-window converter gate (cardfuzz batch3 lines 9/11). T3
// keeps a converter from looping at PRIORITY, but the engine also offers
// mana sources as "activate" options inside KChoose payment windows (the
// cast payment window, the UnlessCost$ and Ward windows, cumulative upkeep),
// and those were answered by a first-"activate" pick. A converter is never
// tapped, so it is re-offered after every activation: once the lands were
// spent, Farrelite Priest's "{1}: Add {W}" paid {1} out of the pool and added
// the {W} back, one decision per loop, until the livelock watcher fired
// (Peacekeeper's upkeep "unless you pay {1}{W}").
//
// A payment window carries no fact naming the charge, so a conversion's
// progress cannot be priced there; the gate fails closed toward Done, the
// T3 direction: the first offered source that is NOT a converter is taken,
// and when only converters remain the window is closed with Done. Every
// non-converter activation taps a source (converterCost's definition), so
// the activations a window can take are bounded by the untapped sources,
// and every window ends. This is a class rule over the option's
// engine-supplied cost marker, read the same way T3 reads it; a window with
// no "activate" option is not a payment window and is left to the caller.
func chooseManaWindow(d *decision.Decision) (int, bool) {
	isWindow := false
	for _, o := range d.Options {
		if o.Kind != "activate" {
			continue
		}
		isWindow = true
		if conv, _ := converterCost(o.Cost); conv {
			continue
		}
		return o.Index, true
	}
	if !isWindow {
		return 0, false
	}
	for _, o := range d.Options {
		if o.Kind == "done" {
			return o.Index, true
		}
	}
	return 0, false
}
