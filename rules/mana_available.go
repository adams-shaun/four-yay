package rules

import (
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// AvailableMana is the engine's answer to the view's "what could this seat
// tap for right now" question (view.Chars). It is the mana the seat could
// produce this very moment by activating the free-to-tap mana abilities of
// permanents it controls. Tapped sources count only when an available ability
// pays {Q}; an unavailable {T} ability on a tapped source is not counted. This
// is the "free mana one gains by tapping lands or other effects" the seat-box
// line 3 advertises, as distinct from
// the floating Pool (poolView), which is a between-decisions snapshot that
// CR 500.4 empties at every step's end.
//
// It is public information: it is derived entirely from the battlefield
// (a public zone), never from a hand, library or graveyard, so the same value
// is projected for every seat and for every visibility (seat, public,
// omniscient).
//
// The eligibility gate mirrors the engine's own tap-for-mana offer
// (rules/legal.go legalActions): a permanent with at least one unrestrictable,
// presently payable mana ability. Beyond that gate this is deliberately
// narrower, in three honest ways the projection must not paper over:
//
//   - Only a mana ability whose activation cost is a bare tap (or free) is
//     counted. manaFreeCost rejects any cost that also sacrifices, removes
//     a counter, pays life or mana -- activating such an ability is not
//     "free mana by tapping". A permanent that is a legitimate tap source
//     but whose only mana ability carries a paid cost contributes nothing.
//   - Ability-restricted mana abilities (CantBeActivated) are skipped, the
//     same restriction legalActions' offer gate consults, so a source a
//     static currently forbids is not advertised as available.
//   - A permanent with several remaining free mana abilities contributes
//     nothing. It can tap for one of those abilities, not their sum, and
//     state.Mana cannot represent that colour choice without falsely calling
//     it colourless or promising both colours. This keeps AvailableMana a
//     conservative fixed-colour lower bound rather than overstating payment.
//   - An Indeterminate Amount$ ("X", "Y", a Count$ expression) yields no
//     amount the seat is guaranteed to receive, so it contributes nothing.
//     (mirrors cards.ManaiProduction, which resolves it to zero rather than
//     claiming a count the pool is never promised.)
//
// Like `Cards`' production, a Produced$ of "Any"/"Combo Any" reports all
// five possible colours and no colourless unit. AvailableMana is an aggregate
// capability vector, not a claim that one tap supplies all five units: the
// activation path still asks which one the player takes. CardView.Produces
// and AvailableMana therefore share the same real alternatives, while the
// pool event records only the selected colour.
func (e *Engine) AvailableMana(p state.PlayerID) state.Mana {
	var out state.Mana
	ids := e.appendBattlefieldManaSourceIDs(e.idsBorrow(), p)
	defer e.idsRelease(ids)
	for _, id := range ids {
		o := e.G.Obj(id)
		if o == nil {
			continue
		}
		f := o.Face()
		if f == nil {
			continue
		}
		var free []*cards.SA
		for _, ma := range e.availableManaAbilities(p, id) {
			cost := e.parseCost(ma.ParamStr(cards.PKCost))
			if manaFreeCost(cost) && !activationTapCostUnavailable(o, &cost) {
				free = append(free, ma)
			}
		}
		// Tapping this permanent selects one ability. A single Any ability can
		// report its five alternatives through ProducedCounts; a permanent with
		// several distinct abilities is omitted because the vector cannot encode
		// which ability the tap will select.
		if len(free) == 1 {
			addAvailable(&out, free[0], e.chosenProducedColour(id))
		}
	}
	return out
}

// battlefieldManaSourceIDs lists the payer's battlefield permanents first,
// followed by other players' battlefield permanents in seat order. The latter
// matter when an ability's Activator$ explicitly permits the payer; the shared
// availableManaAbilities gate filters each source. Zone order within each
// owner is retained for deterministic option ordering.
func (e *Engine) battlefieldManaSourceIDs(p state.PlayerID) []state.ObjID {
	return e.appendBattlefieldManaSourceIDs(nil, p)
}

// appendBattlefieldManaSourceIDs is battlefieldManaSourceIDs appending to
// dst (a list borrowed with idsBorrow by a caller that only ranges it).
func (e *Engine) appendBattlefieldManaSourceIDs(dst []state.ObjID, p state.PlayerID) []state.ObjID {
	ids := append(dst, e.G.Zone(state.ZBattlefield, p)...)
	for _, owner := range e.G.AliveFrom(0) {
		if owner != p {
			ids = append(ids, e.G.Zone(state.ZBattlefield, owner)...)
		}
	}
	return ids
}

// manaSourceIDs adds the payer's non-battlefield printed mana sources to the
// public battlefield set. Hand and graveyard sources remain owner-scoped.
func (e *Engine) manaSourceIDs(p state.PlayerID) []state.ObjID {
	ids := e.battlefieldManaSourceIDs(p)
	for _, z := range []state.Zone{state.ZHand, state.ZGraveyard} {
		ids = append(ids, e.G.Zone(z, p)...)
	}
	return ids
}

// manaFreeCost reports whether a mana ability's activation cost is a bare
// tap (or empty -- an ability that produces mana for nothing): Tap may be
// true, but no Sac, no Discard, no SubCounter, no generic/coloured mana, no variable X.
// A cost like "T, Sac <1/CARDNAME>", "T, PayLife<1>", "2 T" or "G T" is
// not free and must not be counted as available-by-tapping.
func manaFreeCost(c Cost) bool {
	return len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 &&
		len(c.AddCounter) == 0 && len(c.Exile) == 0 && len(c.Reveal) == 0 &&
		len(c.RevealOrChoose) == 0 &&
		len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage &&
		c.Generic == 0 && c.Life == 0 && c.Colored == (state.Mana{}) && c.X == 0 &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0
}

// windowManaAlt is one deterministic production alternative of a single
// permanent: the exact ability resolveManaAbility will resolve (so
// the activation poses no chooseMana sub-ask), its Produced$ colour counts,
// and its literal amount. A permanent that can tap for one of several
// colours (a Volcanic Island's intrinsic {U} and {R} abilities) carries one
// alt per ability, because the tap yields exactly one of them -- never their
// sum. The window's tap list offers one option per alt, so the payer's
// colour choice is made in the decision rather than in a nested ask.
type windowManaAlt struct {
	ma     *cards.SA
	counts [6]int32
	amt    int32
	// any records ProducedCounts' open-choice result.  Consumers which need a
	// concrete witness (payment plans) must decline it even where the ordinary
	// payment window has a deterministic fallback.
	any bool
	// life is the life the activation pays (a PayLife<N> activation cost).
	// Every alt the shared windowManaUnits builds carries 0; only the
	// cast-payment probe's paid-cost layer sets it, so the affordability
	// search can debit that life from the payer's budget -- an activation
	// that spends life must not be promised as if the life were still
	// available for the cost being priced.
	life int32
	// costGeneric is the literal generic mana the activation pays BEFORE the
	// production is added (a "{N}, {T}: add ..." activation cost). Every alt
	// the shared windowManaUnits builds carries 0 (a free tap), so the field
	// is inert for the attack/unless payment windows. The cast-payment
	// probe's paid-cost layer sets it and the cast-only ordered eligibility
	// search (castWindowReachable) pays it from the pool this window has
	// already accumulated, so a generic fee can be funded by an earlier
	// same-window activation -- the exact sequence the live CR 601.2g window
	// can perform, one source at a time. It is deliberately NOT netted into
	// counts/amt: a multi-colour production cannot express "minus N" without
	// choosing which colour the generic consumed, and the choice is the
	// payer's at activation time.
	costGeneric int32
}

// mana is the alt's production as a mana vector, the form the walk's
// affordability search and the window's safety ordering both add to a pool.
func (a windowManaAlt) mana() state.Mana {
	var m state.Mana
	for i, n := range a.counts {
		m[state.ManaIndex(cards.ManaSymbol(i))] += n * a.amt
	}
	return m
}

// windowManaUnit is one permanent as a PAYMENT WINDOW sees it: its
// single tap's production ALTERNATIVES. freeCount is the number of
// free-cost, window-usable abilities the permanent has BEFORE the
// per-ability priceability filter, so a consumer that must tap exactly one
// ability without a sub-ask (the attack-cost window) can require freeCount
// == 1 && len(alts) == 1, reproducing the pre-alternatives membership
// exactly. It is the shared membership behind every payment window that
// must not promise more than it can tap -- the declare-attackers attack-cost
// window (attackManaSources) and the mid-resolution unless-cost window
// (UnlessCostPayable / askUnlessMana), so their offer gates and their tap
// lists cannot drift apart.
type windowManaUnit struct {
	id        state.ObjID
	freeCount int
	alts      []windowManaAlt
}

// windowManaUnits walks p's battlefield in zone order and returns every
// permanent whose PAYMENT-WINDOW mana abilities include at least one
// free-cost ability whose production this build can price deterministically.
// It is deliberately narrower than untappedManaSource in three honest ways a
// payment-window affordability bound must honour:
//
//   - the abilities come from availableManaAbilitiesForWindow(p, id, false),
//     so an InstantSpeed$ True ability (Lion's Eye Diamond, "Activate only as
//     an instant") is withheld -- the window genuinely cannot activate it, so
//     counting it would let the offer gate promise mana the window cannot
//     tap (the review's stranding defect);
//   - a RestrictValid$-governed ability is excluded: its produced batch may
//     not pay the cost, so counting its units would overstate reach;
//   - an Indeterminate Amount$ ("X", "Y", a Count$) yields no guaranteed
//     amount, and a choice-shaped production ("Combo B R", "Chosen") names
//     no single colour -- forward-direction symbols ("G", "R G", "RR") and
//     the executor's own blank/"Any"/"Combo Any" one-colourless default are
//     the only deterministic shapes. The default is returned as counts[5]==1
//     (the same colourless slot AvailableMana folds it into) so a consumer
//     that needs a colour vector (UnlessCostPayable) sees exactly what the
//     window can produce.
//
// A permanent with several free abilities is NOT dropped: each priceable
// ability becomes one alt, since the permanent still taps for one of them.
//
// The walk is a pure read, so it runs in one Derived memo scope
// (rules/derivedmemo.go): each permanent's window-ability walk reads the
// board's Continuous statics, and inside the scope that battlefield scan is
// served from the walk cache (rules/walkcache.go) rather than repeated per
// permanent -- O(permanents^2) per call on a token board otherwise (cardfuzz
// seed 6181111140895991800).
func (e *Engine) windowManaUnits(p state.PlayerID) []windowManaUnit {
	return e.windowManaUnitsOnly(p, nil)
}

// windowManaUnitsOnly is windowManaUnits restricted to the sources in only
// (nil: every source): each source's unit is computed from that source
// alone, so the restricted census is exactly the full census's units for
// those sources, in the same order.
func (e *Engine) windowManaUnitsOnly(p state.PlayerID, only []state.ObjID) []windowManaUnit {
	return e.windowManaUnitsWith(p, only, nil)
}

// windowManaUnitsWith is windowManaUnitsOnly with p's own battlefield
// sources' payment-window abilities already read: pre[k] is
// availableManaAbilitiesForWindow(p, id, false) for the k-th object of p's
// battlefield zone (the first ids the walk visits, appendBattlefieldManaSourceIDs),
// read at this state by a caller that needs the same lists (the payment
// planner's census). The list is a pure read of the board, so it is the
// one this walk would read.
func (e *Engine) windowManaUnitsWith(p state.PlayerID, only []state.ObjID, pre [][]*cards.SA) []windowManaUnit {
	e.beginDerivedMemo()
	defer e.endDerivedMemo()
	var out []windowManaUnit
	var flat []windowManaAlt
	var boundsBuf [64]int32
	bounds := boundsBuf[:0] // each unit's first alt in flat
	ids := e.appendBattlefieldManaSourceIDs(e.idsBorrow(), p)
	defer e.idsRelease(ids)
	for k, id := range ids {
		if only != nil && !slices.Contains(only, id) {
			continue
		}
		o := e.G.Obj(id)
		if o == nil || o.Face() == nil {
			continue
		}
		var mas []*cards.SA
		if k < len(pre) {
			mas = pre[k]
		} else {
			mas = e.availableManaAbilitiesForWindow(p, id, false)
		}
		// One pass: each free ability counts toward freeCount and, when its
		// production prices deterministically, becomes the next alt (the
		// same alts, in the same order, as filtering the free list first).
		// The alts of every unit share one backing array, cut into capped
		// spans once the walk is done (a later append to one unit's alts
		// reallocates instead of writing into the next unit's).
		start, free := len(flat), 0
		for _, ma := range mas {
			// The ability's own text reads come from its configured facts
			// (manaStaticOf).
			mf := e.manaStaticOf(ma)
			if mf.restrictValid {
				continue
			}
			if !mf.freeCost || (mf.tap && o.Tapped) || (mf.untap && !o.Tapped) {
				continue // activationTapCostUnavailable
			}
			free++
			amt := mf.amount
			if amt <= 0 {
				continue
			}
			counts, any := mf.counts, mf.any
			total := int32(0)
			for _, n := range counts {
				total += n
			}
			if any {
				// Only the executor's deterministic one-colourless default counts.
				if total != 1 || counts[5] != 1 {
					continue
				}
			} else if total <= 0 {
				continue
			}
			flat = append(flat, windowManaAlt{ma: ma, counts: counts, amt: amt, any: any})
		}
		if len(flat) == start {
			continue
		}
		out = append(out, windowManaUnit{id: id, freeCount: free})
		bounds = append(bounds, int32(start))
	}
	bounds = append(bounds, int32(len(flat)))
	for i := range out {
		s, t := bounds[i], bounds[i+1]
		out[i].alts = flat[s:t:t]
	}
	return out
}

// addMana returns a+b elementwise.
func manaAdd(a, b state.Mana) state.Mana {
	var m state.Mana
	for i := range m {
		m[i] = a[i] + b[i]
	}
	return m
}

// addAvailable folds one free-to-tap mana ability into an available-mana
// accumulator through cards.ProducedCounts -- the ONE Produced$ parse the
// per-face projection (cards.ManaProduction.add) and this aggregate share,
// so the two agree by construction: blank becomes one colourless,
// Any/Combo Any expose their possible WUBRG colours, a plain
// symbol token adds its listed colours ("Combo B R" one B and one R, "RR"
// two red), and an unrecognised token ("ColorIdentity", a "Special ..."
// word) claims no mana at all -- never the phantom colourless a rune walk of
// the word itself used to count. The amount comes from Amount$ with the
// executor's default of 1 and the T14-f negative clamp; an Indeterminate
// amount ("X", "Y", a Count$, "Sacrificed$...") resolves to zero,
// contributing nothing.
//
// chosen is the source's recorded as-enters colour (state.Object.ChosenColor,
// a single WUBRG letter or "") and is substituted into a "Chosen"/"Combo <C>
// Chosen" production BEFORE the parse (substituteChosenProduced, the same
// read the activation path performs). Without it a "Combo R Chosen" permanent
// whose recorded colour is G would be advertised as all five colours, when it
// can currently produce only R or G. ProducedCounts itself still reports the
// five-colour superset for a bare "Chosen" because it has no source object;
// the substitution here is what supplies the source-aware answer.
func addAvailable(m *state.Mana, ma *cards.SA, chosen string) {
	mp := effects.ManaOf(ma)
	raw := mp.Produced
	produced := substituteChosenProduced(raw, chosen)
	// ProducedCounts intentionally has no source and therefore exposes the
	// WUBRG superset for a raw Chosen token. This source-aware projection has
	// one: without its recorded as-enters choice the activation fails closed,
	// so it must advertise nothing rather than that hypothetical superset.
	if producedNeedsChosen(raw) && produced == raw {
		return
	}
	counts, _ := cards.ProducedCounts(produced)
	amt := availableAmountOf(mp)
	for i, n := range counts {
		m[state.ManaIndex(cards.ManaSymbol(i))] += n * amt
	}
}

// availableAmount is cards' manaAbilityAmount, re-derived here because that
// helper is unexported and this package needs a per-ability amount to fold a
// single ability into an accumulator. A blank Amount is the executor's own
// default of 1; a literal integer is used directly (negative clamped to 0);
// anything else is a value the projection cannot statically price, so it
// returns 0 -- never a count the pool is not guaranteed to receive.
func availableAmount(ma *cards.SA) int32 { return availableAmountOf(effects.ManaOf(ma)) }

// availableAmountOf is availableAmount over compiled production parameters.
func availableAmountOf(mp *effects.ManaParams) int32 {
	if mp.AmountTrim == "" {
		return 1
	}
	if v := mp.AmountLit; mp.AmountIsLit {
		if v < 0 {
			return 0
		}
		return int32(v)
	}
	return 0
}
