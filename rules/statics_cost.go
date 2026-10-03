package rules

import (
	"fmt"
	"math"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/chars"
	"github.com/adams-shaun/gorge/state"
)

// costScope names WHAT is being priced when cost modifiers are collected:
// a spell cast (kind "Spell", with the cast variant mode naming the
// flashback/surge/kicked/miracle shape a ValidSpell$ may gate on) or an
// activated ability (kind "Ability", with ab the exact SA being activated —
// the thing a ValidSpell$ Activated.<keyword> static scopes to). The zero
// kind is "Spell"; mode "" is the plain cast.
type costScope struct {
	kind string
	mode string
	ab   *cards.SA
}

func spellScope(mode string) costScope    { return costScope{kind: "Spell", mode: mode} }
func foretellScope() costScope            { return costScope{kind: "Foretell", mode: "foretell"} }
func abilityScope(ab *cards.SA) costScope { return costScope{kind: "Ability", ab: ab} }

// castSaMayPlaySource is the card-level MayPlaySource token a cost static's
// ValidCard$ may carry (castSaTokens' flag entry of the same spelling).
const castSaMayPlaySource = "CastSa Spell.MayPlaySource"

// specialActionScope prices a CR 116.2 special action that is neither a
// spell nor an activated ability but still has a cost the CR 601.2f
// modifiers reach -- Forge prices these as static abilities (ValidSpell$
// Static.<X>). The modes: "unlock" (CR 309.5 Room unlock), and the CR 708.6
// turn-face-up actions "morphup" (Morph and Megamorph -- Forge's isMorphUp)
// and "disguiseup" (Disguise). Kind "Static" is never Type$ Spell or Type$
// Ability (Forge's Type$ gate requires isSpell / isActivatedAbility), carries
// no commander tax and no targets.
func specialActionScope(mode string) costScope { return costScope{kind: "Static", mode: mode} }

// modeIsCastFaceDown reports whether a cast mode puts the spell on the stack
// face down (CR 708.4): the morph family's {3} cast. It is the one reading of
// Forge's SpellAbility.isCastFaceDown the ValidSpell$ Spell.isCastFaceDown
// constraint shares with the cast flow's own faceDown mark (beginCast sets
// pendingCast.faceDown for exactly these modes).
func modeIsCastFaceDown(mode string) bool {
	if v, ok := modeIsCastFaceDownTab1.Get(mode); ok {
		return v
	}
	return false
}

// costMod is ONE evaluated ReduceCost static's contribution to a total cost.
// generic is the literal/SVar-evaluated Amount$; colored carries a Color$
// reduction (per colour slot, set only when the static names one);
// ignoreGeneric records the IgnoreGeneric$ flag (a colour reduction whose
// pip the cost lacks spills to generic only when this is false); floor is
// the MinMana$ lower bound ("can't reduce the mana in that cost to less than
// N mana") evaluated AFTER this reduction alone.
type costMod struct {
	generic       int32
	colored       state.Mana
	hasColor      bool
	ignoreGeneric bool
	floor         int32
}

// costMods is the evaluated composition for one cast/activation, in the CR
// 601.2f order: every increase first, then every reduction in static order
// (each with its own MinMana floor), then a SetCost floor last.
type costMods struct {
	raises []int32
	extra  Cost
	// raiseCol / raiseLife carry the aggregated RaiseCost Cost$ raises: the
	// coloured pips, generic and life a "Black spells you cast cost {B} more"
	// static adds to the total (an additional cost — never reduced by the
	// reductions that follow, per CR 601.2f's increase-then-reduce order).
	raiseCol  state.Mana
	raiseGen  int32
	raiseLife int32
	reduces   []costMod
	setFloor  int32
	// hasExtra records that extra was ever composed into (the only writes
	// that can make it non-zero). While it is false extra is the zero Cost,
	// which apply may then skip (see apply).
	hasExtra bool
	// waterbend / waterbendX / waterbendPartX / raiseX carry a RaiseCost
	// Cost$ Waterbend<N> or Waterbend<X> (Water Whip, Crashing Wave) and a
	// cost's OWN Waterbend<N>/<X> part (Giant Koi's ability, a self-spell
	// OptionalCost; the keyword action "waterbend {N}": pay {N}, and each
	// untapped artifact or creature you tap while paying it pays for {1}).
	// The fixed {N} is in raiseGen or the cost itself; waterbend is how much
	// of the generic total those taps may cover. raiseX counts the RaiseCost
	// Waterbend<X> parts (each adds an {X} to the pending cost, folded at the
	// fold site since xAsk reads pc.cost.X); waterbendPartX counts the
	// Waterbend<X> parts carried by the COST itself, whose amount is the same
	// announced X (foldRaiseExtra does not add those to raiseX, since
	// ParseCost already counted them into cost.X). waterbendX lets the taps
	// cover the announced X at all. apply reads none of them -- the {N}
	// already rides raiseGen and an unannounced {X} prices at zero; every
	// cap site reads the one waterbendCap helper.
	waterbend      int32
	waterbendX     bool
	waterbendPartX int32
	raiseX         int32
}

// empty reports whether the composition would change nothing, so a caller can
// keep its old zero-value shorthand.
func (m costMods) empty() bool {
	return len(m.raises) == 0 && !m.hasExtra && m.raiseGen == 0 && m.raiseLife == 0 &&
		m.raiseCol.Total() == 0 && len(m.reduces) == 0 && m.setFloor == 0
}

// reduceTotal is the most generic mana the composition's reductions can take
// off a cost: each reduction's generic amount plus its colour amounts (a
// colour shortfall spills to generic). A caller bounding an {X} search adds
// it to the ceiling, since a reduction only makes a larger X cheaper.
func (m costMods) reduceTotal() int32 {
	var n int64
	for _, red := range m.reduces {
		n += int64(red.generic) + int64(red.colored.Total())
	}
	if n > math.MaxInt32/2 {
		n = math.MaxInt32 / 2
	}
	return int32(n)
}

// apply composes c with the modifiers, CR 601.2f: increases before
// reductions, each reduction applied whole (Color$ reductions take the
// matching pips first, spilling the shortfall to generic unless
// IgnoreGeneric$), each reduction's MinMana$ floor restored immediately, and
// the SetCost floor raised to last (Trinisphere: total mana below 3 becomes
// 3). Generic never dips below zero at any point.
func (m costMods) apply(c Cost) Cost {
	m.applyTo(&c)
	return c
}

// applyTo is apply composing *c in place: the one body, so the offer gate's
// pointer path (composeFeasibleP) and every value caller compose alike
// without copying the ~800-byte Cost and ~900-byte costMods per call.
func (m *costMods) applyTo(c *Cost) {
	// Plus with the zero Cost is the identity on every field except the
	// three it clamps or maxes at zero (Life, Snow, XMin), so the two
	// 744-byte copies are skipped only when extra is provably zero and none
	// of those three is negative.
	if m.hasExtra || c.Life < 0 || c.Snow < 0 || c.XMin < 0 {
		*c = plusRaiseExtra(*c, m.extra)
	}
	for _, r := range m.raises {
		c.Generic = addClampedGeneric(c.Generic, int64(r))
	}
	for i := range m.raiseCol {
		c.Colored[i] = addClampedGeneric(c.Colored[i], int64(m.raiseCol[i]))
	}
	c.Generic = addClampedGeneric(c.Generic, int64(m.raiseGen))
	c.Life = addClampedGeneric(c.Life, int64(m.raiseLife))
	for _, red := range m.reduces {
		leftover := red.generic
		if red.hasColor {
			for i := range red.colored {
				take := red.colored[i]
				if c.Colored[i] < take {
					take = c.Colored[i]
				}
				c.Colored[i] -= take
				leftover += red.colored[i] - take
			}
		}
		if leftover > 0 && !(red.hasColor && red.ignoreGeneric) {
			c.Generic -= leftover
			if c.Generic < 0 {
				c.Generic = 0
			}
		}
		if red.floor > 0 {
			if total := totalMana(*c); total < red.floor {
				c.Generic = addClampedGeneric(c.Generic, int64(red.floor-total))
			}
		}
	}
	if m.setFloor > 0 {
		if total := totalMana(*c); total < m.setFloor {
			c.Generic = addClampedGeneric(c.Generic, int64(m.setFloor-total))
		}
	}
}

// totalMana counts the mana a cost demands, pip by pip — every symbol CR
// 107.4 knows counts one, and numeric tokens count their value. This is the
// total the MinMana$ and SetCost floors are measured against.
func totalMana(c Cost) int32 {
	return c.CMC()
}

// hasFloor reports whether the composition carries any MinMana$ or SetCost
// floor. A floor raises the cost's total mana (Cost.CMC) to a minimum, and
// CR 202.4b prices a monocolour-hybrid pip at its generic face — the highest
// of its faces — so an unresolved pip overprices every cheaper face the
// announcement may resolve it to. That is the one part of the composition
// whose result depends on the not-yet-announced pip faces; raises and
// reductions are face-independent, so only a composition with a floor needs
// the per-face enumeration feasibleAny performs.
func (m costMods) hasFloor() bool { return m.hasFloorP() }

// hasFloorP is hasFloor without the receiver copy.
func (m *costMods) hasFloorP() bool {
	if m.setFloor > 0 {
		return true
	}
	for _, red := range m.reduces {
		if red.floor > 0 || red.hasColor {
			return true
		}
	}
	return false
}

// composeFeasible is feasibleAny's leaf composition of the cost it prices:
// the XMin announcement floor, the modifiers, the commander tax and the delve
// credit, in that order.
func (m costMods) composeFeasible(c Cost, taxGeneric, delve int32) Cost {
	return composeFeasibleP(&m, &c, taxGeneric, delve)
}

// composeFeasibleP is composeFeasible over pointers (the offer gate's hot
// path): *c and *m are only read.
func composeFeasibleP(m *costMods, c *Cost, taxGeneric, delve int32) Cost {
	// A cost carrying an XMin<N> lower bound is priced at its smallest
	// LEGAL announcement: "X can't be 0" means the offer must be able
	// to pay {X}=XMin, never {X}=0 (Thieving Skydiver's kicked Kicker).
	// The fold is on a LOCAL copy, so the payment descriptor's
	// announced-X marker (set from the raw cost's own Cost.X by
	// paymentFor) still reports CostContainsX. WithX clears XMin, so
	// an already-announced cost (XMin==0) is untouched here.
	var cc Cost
	if c.XMin > 0 {
		cc = c.WithX(c.XMin)
	} else {
		cc = *c
	}
	m.applyTo(&cc)
	cc.Generic = addClampedGeneric(cc.Generic, int64(taxGeneric))
	if cc.Generic > delve {
		cc.Generic -= delve
	} else {
		cc.Generic = 0
	}
	return cc
}

// feasibleAny is THE one shared mana-feasibility primitive of the cast flow.
// It answers the CR 601.2b/601.2f question for a cost whose flexible pips may
// still be unresolved — at the offer gate (offerCastable), at each CR 601.2b
// announcement menu (announceFeasible, with the pips already announced folded
// in as their final resolved faces) and at the target-repricing gates (a cost
// with no live pip, where it degenerates to the composed payable check): is
// there SOME legal face assignment of the remaining announcement pips
// (two-colour hybrid, monocolour hybrid, Phyrexian, hybrid-Phyrexian) that,
// with m composed onto the RESOLVED faces at the leaf of the walk and
// taxGeneric added after (an additional cost is never reduced), is payable
// from pool/snow/life? Because the offer gate, every announcement menu and
// the charge (manaToPay/payMana) compose the same modifiers through this one
// primitive, an offered cast, an offered announcement face and the charged
// total can never disagree.
// delve is the payer's Delve graveyard credit pool (0 without Delve): the
// credit is taken off each assignment's generic after the composition and
// tax — the same place the payment (payCast) takes it off manaToPay.
//
// A cost with no announcement pip, or a composition with no face-sensitive
// modifier, composes identically for every face and degrades to the single
// composed payable check. Without the enumeration an offer priced under a
// floor with an unresolved twobrid pip could be offered although NEITHER of
// its announced faces completes the raised cost — Trinisphere over an
// unresolved {2/W} composes {2/W}+{1} (payable from {W}{W} by its white face
// plus one generic), while the white face announces to {W}+{2} and the
// generic face to {3}, neither payable from {W}{W}. Color$ needs the same
// ordering: a W reduction must see an announced W half of {W/U}. The walk
// resolves one pip per level in announcePip order and stops at the first
// payable assignment, so a payable cost is found without visiting the whole
// tree.
func (m costMods) feasibleAny(c Cost, pool, snow state.Mana, typed [7]state.Mana, life, taxGeneric, delve int32, bLifeOK bool, rider pipRider, conv *manaConv) bool {
	composed := func(c Cost) bool {
		cc := m.composeFeasible(c, taxGeneric, delve)
		// The pool-unit floor is a necessary condition of resolveManaWith's
		// own search (poolUnitsFloor), decided without building its pips.
		if int64(pool.Total()) < cc.PoolUnitsFloor() {
			return false
		}
		_, ok := resolveManaWith(cc, pool, snow, typed, life, bLifeOK, rider, conv)
		return ok
	}
	if !m.hasFloor() || c.AnnPipCount() == 0 {
		return composed(c)
	}
	var walk func(c Cost) bool
	walk = func(c Cost) bool {
		if c.AnnPipCount() == 0 {
			return composed(c)
		}
		for _, alt := range announcePip(c, 0) {
			r := c
			switch {
			case alt.Color != 0:
				r.Colored[state.ManaIndex(alt.Color)]++
			case alt.Generic > 0:
				r.Generic = addClampedGeneric(r.Generic, int64(alt.Generic))
			case alt.Life > 0:
				r.Life = addClampedGeneric(r.Life, int64(alt.Life))
			}
			if walk(r.DropAnnouncePrefix(1)) {
				return true
			}
		}
		return false
	}
	return walk(c)
}

// manaFeasibleGrant is manaFeasible with the may-play ignore-colour rider
// passed explicitly (a pendingCast's pc.mayPlayIgnore, or the offer-side
// payerGrantsIgnoreColor derivation), and the payer's PayLifeInsteadOf:B
// grant derived here. Both widen the leaf payable check the same way the
// payment (resolveManaWith) widens it, so an offered cast, an offered
// announcement face and the charged total can never disagree on a
// K'rrik-shaped or MayPlayIgnoreColor$-shaped cost either. The descriptor is
// the caller's: post-announcement costs (pc.resolvedMana and friends) have
// WithX folded their X into Generic, so they must arrive through
// paymentForCast — a paymentFor-built descriptor from a folded cost would
// hide every CostContainsX batch from the target-repricing gates exactly
// where the offer and the payment both admit it.
func (e *Engine) manaFeasibleDescriptor(p state.PlayerID, d paymentDescriptor, c Cost, mods costMods, taxGeneric, delve int32, rider pipRider) bool {
	pl := e.G.Players[p]
	av := e.manaAvailableFor(p, d)
	return mods.feasibleAny(c, av.pool, pl.Snow, av.typed, pl.Life, taxGeneric, delve,
		e.payerGrantsPayLifeInsteadOfB(p), rider, e.paymentConv(p, d.id, d.class == paymentActivated))
}

// manaFeasiblePool is manaFeasible priced against an EXPLICIT pool instead of
// the seat's restriction-adjusted floating one. The ordinary gate
// (manaFeasible above) is exactly this with the real manaAvailableFor pool;
// the potential-action walk (rules/legal.go legalActionsPriced) passes the
// hypothetical bound the seat would hold after floating every untapped
// source. The payer grants and conversion shaping are the same reads in both
// modes, so a potential action and the offer the walk mirrors can never
// disagree about what the pool may satisfy.
func (e *Engine) manaFeasiblePool(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, pool state.Mana, typed [7]state.Mana) bool {
	return e.manaFeasiblePoolP(p, id, ability, &c, &mods, taxGeneric, delve, pool, typed)
}

// manaFeasiblePoolP is manaFeasiblePool over pointers: *c and *mods are only
// read, and the common pool-floor refusal never copies either.
func (e *Engine) manaFeasiblePoolP(p state.PlayerID, id state.ObjID, ability bool, c *Cost, mods *costMods, taxGeneric, delve int32, pool state.Mana, typed [7]state.Mana) bool {
	// Without an announcement walk feasibleAny is exactly its composed leaf,
	// whose first answer is the pool-unit floor (poolUnitsFloor): decide that
	// here, before the payer's grant and conversion reads the search needs.
	if !mods.hasFloorP() || c.AnnPipCountP() == 0 {
		if int64(pool.Total()) < composedPoolFloor(mods, c, taxGeneric, delve) {
			return false
		}
	}
	pl := &e.G.Players[p]
	return mods.feasibleAny(*c, pool, pl.Snow, typed, pl.Life, taxGeneric, delve,
		e.payerGrantsPayLifeInsteadOfB(p),
		pipRider{AnyColor: e.payerGrantsIgnoreColor(p, id), AnyType: e.payerGrantsIgnoreType(p, id)},
		e.paymentConv(p, id, ability))
}

// manaFeasiblePriced is manaFeasible's priced-mode entry: hyp nil keeps the
// ordinary real-pool gate, hyp non-nil prices the feasibility against the
// potential walk's hypothetical bound (rules/legal.go legalActionsPriced).
func (e *Engine) manaFeasiblePriced(p state.PlayerID, id state.ObjID, ability bool, c Cost, mods costMods, taxGeneric, delve int32, hyp *state.Mana) bool {
	return e.manaFeasiblePricedP(p, id, ability, &c, &mods, taxGeneric, delve, hyp)
}

// manaFeasiblePricedP is manaFeasiblePriced over pointers (*c and *mods are
// only read). The priced pool is manaAvailableFor's, whose descriptor matters
// only to a RestrictedMana batch: without one it is the floating pool and the
// raw tally verbatim, and under hyp both are replaced outright, so neither
// case builds the descriptor (a Cost copy).
func (e *Engine) manaFeasiblePricedP(p state.PlayerID, id state.ObjID, ability bool, c *Cost, mods *costMods, taxGeneric, delve int32, hyp *state.Mana) bool {
	pl := &e.G.Players[p]
	var pool state.Mana
	var typed [7]state.Mana
	switch {
	case hyp != nil:
		// A hypothetical bound is a pure mana bound (see costPayablePool),
		// so its typed partition is the raw tally.
		pool, typed = *hyp, pl.ManaUnits()
	case len(pl.RestrictedMana) == 0:
		pool, typed = pl.Pool, pl.ManaUnits()
	default:
		av := e.manaAvailableFor(p, paymentFor(id, ability, *c))
		pool, typed = av.pool, av.typed
	}
	return e.manaFeasiblePoolP(p, id, ability, c, mods, taxGeneric, delve, pool, typed)
}

// effectZoneOK reports whether a static whose EffectZone$ reads v applies
// while its source sits in zone z. Forge's default is the battlefield, and
// the corpus names zones with Forge's comma-separated All/Battlefield/Stack/
// Graveyard/Hand/Library/Exile/Command words; anything unrecognised denies
// (the same fail-closed direction every unparseable static qualifier takes),
// because wrongly applying a hand-or-library static is exactly the class of
// over-reach the zone gate exists to prevent.
func effectZoneOK(v string, z state.Zone) bool { return chars.EffectZoneOK(v, z) }

// composedPoolFloor is composeFeasibleP(m, c, taxGeneric, delve)
// .PoolUnitsFloor(). Under the zero composition (costModsZero) on a cost
// with no XMin and no negative Life/Snow, apply only clamps Generic and the
// coloured pips at zero, so the floor is computed from *c directly instead
// of materialising the composed Cost; walkSkipVerify checks it against the
// composition.
func composedPoolFloor(m *costMods, c *Cost, taxGeneric, delve int32) int64 {
	if !costModsZero(m) || c.XMin != 0 || c.Life < 0 || c.Snow < 0 {
		cc := composeFeasibleP(m, c, taxGeneric, delve)
		return cc.PoolUnitsFloor()
	}
	g := addClampedGeneric(addClampedGeneric(c.Generic, 0), int64(taxGeneric))
	if g > delve {
		g -= delve
	} else {
		g = 0
	}
	n := int64(g)
	for _, letter := range pipLetters {
		if letter == 'B' {
			continue
		}
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int64(k)
		}
	}
	if walkSkipVerify {
		if cc := composeFeasibleP(m, c, taxGeneric, delve); cc.PoolUnitsFloor() != n {
			panic(fmt.Sprintf("rules: zero-composition pool floor %d disagrees with the composed %d", n, cc.PoolUnitsFloor()))
		}
	}
	return n
}

var modeIsCastFaceDownTab1 = cards.NewStrTable[bool](
	cards.StrEntry[bool]{Key: "morphed", Val: true},
	cards.StrEntry[bool]{Key: "megamorphed", Val: true},
	cards.StrEntry[bool]{Key: "disguised", Val: true},
)
