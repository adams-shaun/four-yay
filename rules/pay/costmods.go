package pay

import (
	"math"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// costmods.go is the cost-modifier vocabulary (lasagna spec W5 E7, the
// statics_cost machinery's data half): what is being priced (CostScope), one
// evaluated reduction (CostMod) and the CR 601.2f composition (CostMods) with
// its pure apply and feasibility reads. Collecting the live statics into a
// CostMods stays in package rules.

// CostScope names WHAT is being priced when cost modifiers are collected:
// a spell cast (kind "Spell", with the cast variant mode naming the
// flashback/surge/kicked/miracle shape a ValidSpell$ may gate on) or an
// activated ability (kind "Ability", with ab the exact SA being activated —
// the thing a ValidSpell$ Activated.<keyword> static scopes to). The zero
// kind is "Spell"; mode "" is the plain cast.
type CostScope struct {
	Kind string
	Mode string
	Ab   *cards.SA
}

// CostMod is ONE evaluated ReduceCost static's contribution to a total cost.
// generic is the literal/SVar-evaluated Amount$; colored carries a Color$
// reduction (per colour slot, set only when the static names one);
// ignoreGeneric records the IgnoreGeneric$ flag (a colour reduction whose
// pip the cost lacks spills to generic only when this is false); floor is
// the MinMana$ lower bound ("can't reduce the mana in that cost to less than
// N mana") evaluated AFTER this reduction alone.
type CostMod struct {
	Generic       int32
	Colored       state.Mana
	HasColor      bool
	IgnoreGeneric bool
	Floor         int32
}

// CostMods is the evaluated composition for one cast/activation, in the CR
// 601.2f order: every increase first, then every reduction in static order
// (each with its own MinMana floor), then a SetCost floor last.
type CostMods struct {
	Raises []int32
	Extra  Cost
	// RaiseCol / raiseLife carry the aggregated RaiseCost Cost$ raises: the
	// coloured pips, generic and life a "Black spells you cast cost {B} more"
	// static adds to the total (an additional cost — never reduced by the
	// reductions that follow, per CR 601.2f's increase-then-reduce order).
	RaiseCol  state.Mana
	RaiseGen  int32
	RaiseLife int32
	Reduces   []CostMod
	SetFloor  int32
	// HasExtra records that extra was ever composed into (the only writes
	// that can make it non-zero). While it is false extra is the zero Cost,
	// which apply may then skip (see apply).
	HasExtra bool
	// Waterbend / waterbendX / waterbendPartX / raiseX carry a RaiseCost
	// Cost$ Waterbend<N> or Waterbend<X> (Water Whip, Crashing Wave) and a
	// cost's OWN Waterbend<N>/<X> part (Giant Koi's ability, a self-spell
	// OptionalCost; the keyword action "Waterbend {N}": pay {N}, and each
	// untapped artifact or creature you tap while paying it pays for {1}).
	// The fixed {N} is in raiseGen or the cost itself; Waterbend is how much
	// of the generic total those taps may cover. raiseX counts the RaiseCost
	// Waterbend<X> parts (each adds an {X} to the pending cost, folded at the
	// fold site since xAsk reads pc.cost.X); waterbendPartX counts the
	// Waterbend<X> parts carried by the COST itself, whose amount is the same
	// announced X (foldRaiseExtra does not add those to raiseX, since
	// ParseCost already counted them into cost.X). waterbendX lets the taps
	// cover the announced X at all. apply reads none of them -- the {N}
	// already rides raiseGen and an unannounced {X} prices at zero; every
	// cap site reads the one waterbendCap helper.
	Waterbend      int32
	WaterbendX     bool
	WaterbendPartX int32
	RaiseX         int32
}

// Empty reports whether the composition would change nothing, so a caller can
// keep its old zero-value shorthand.
func (m CostMods) Empty() bool {
	return len(m.Raises) == 0 && !m.HasExtra && m.RaiseGen == 0 && m.RaiseLife == 0 &&
		m.RaiseCol.Total() == 0 && len(m.Reduces) == 0 && m.SetFloor == 0
}

// ReduceTotal is the most generic mana the composition's reductions can take
// off a cost: each reduction's generic amount plus its colour amounts (a
// colour shortfall spills to generic). A caller bounding an {X} search adds
// it to the ceiling, since a reduction only makes a larger X cheaper.
func (m CostMods) ReduceTotal() int32 {
	var n int64
	for _, red := range m.Reduces {
		n += int64(red.Generic) + int64(red.Colored.Total())
	}
	if n > math.MaxInt32/2 {
		n = math.MaxInt32 / 2
	}
	return int32(n)
}

// Apply composes c with the modifiers, CR 601.2f: increases before
// reductions, each reduction applied whole (Color$ reductions take the
// matching pips first, spilling the shortfall to generic unless
// IgnoreGeneric$), each reduction's MinMana$ floor restored immediately, and
// the SetCost floor raised to last (Trinisphere: total mana below 3 becomes
// 3). Generic never dips below zero at any point.
func (m CostMods) Apply(c Cost) Cost {
	m.ApplyTo(&c)
	return c
}

// ApplyTo is apply composing *c in place: the one body, so the offer gate's
// pointer path (composeFeasibleP) and every value caller compose alike
// without copying the ~800-byte Cost and ~900-byte CostMods per call.
func (m *CostMods) ApplyTo(c *Cost) {
	// Plus with the zero Cost is the identity on every field except the
	// three it clamps or maxes at zero (Life, Snow, XMin), so the two
	// 744-byte copies are skipped only when extra is provably zero and none
	// of those three is negative.
	if m.HasExtra || c.Life < 0 || c.Snow < 0 || c.XMin < 0 {
		*c = PlusRaiseExtra(*c, m.Extra)
	}
	for _, r := range m.Raises {
		c.Generic = cost.AddClampedGeneric(c.Generic, int64(r))
	}
	for i := range m.RaiseCol {
		c.Colored[i] = cost.AddClampedGeneric(c.Colored[i], int64(m.RaiseCol[i]))
	}
	c.Generic = cost.AddClampedGeneric(c.Generic, int64(m.RaiseGen))
	c.Life = cost.AddClampedGeneric(c.Life, int64(m.RaiseLife))
	for _, red := range m.Reduces {
		leftover := red.Generic
		if red.HasColor {
			for i := range red.Colored {
				take := red.Colored[i]
				if c.Colored[i] < take {
					take = c.Colored[i]
				}
				c.Colored[i] -= take
				leftover += red.Colored[i] - take
			}
		}
		if leftover > 0 && !(red.HasColor && red.IgnoreGeneric) {
			c.Generic -= leftover
			if c.Generic < 0 {
				c.Generic = 0
			}
		}
		if red.Floor > 0 {
			if total := totalMana(*c); total < red.Floor {
				c.Generic = cost.AddClampedGeneric(c.Generic, int64(red.Floor-total))
			}
		}
	}
	if m.SetFloor > 0 {
		if total := totalMana(*c); total < m.SetFloor {
			c.Generic = cost.AddClampedGeneric(c.Generic, int64(m.SetFloor-total))
		}
	}
}

// totalMana counts the mana a cost demands, pip by pip — every symbol CR
// 107.4 knows counts one, and numeric tokens count their value. This is the
// total the MinMana$ and SetCost floors are measured against.
func totalMana(c Cost) int32 {
	return c.CMC()
}

// HasFloor reports whether the composition carries any MinMana$ or SetCost
// floor. A floor raises the cost's total mana (Cost.CMC) to a minimum, and
// CR 202.4b prices a monocolour-hybrid pip at its generic face — the highest
// of its faces — so an unresolved pip overprices every cheaper face the
// announcement may resolve it to. That is the one part of the composition
// whose result depends on the not-yet-announced pip faces; raises and
// reductions are face-independent, so only a composition with a floor needs
// the per-face enumeration feasibleAny performs.
func (m CostMods) HasFloor() bool { return m.HasFloorP() }

// HasFloorP is hasFloor without the receiver copy.
func (m *CostMods) HasFloorP() bool {
	if m.SetFloor > 0 {
		return true
	}
	for _, red := range m.Reduces {
		if red.Floor > 0 || red.HasColor {
			return true
		}
	}
	return false
}

// ComposeFeasible is feasibleAny's leaf composition of the cost it prices:
// the XMin announcement floor, the modifiers, the commander tax and the delve
// credit, in that order.
func (m CostMods) ComposeFeasible(c Cost, taxGeneric, delve int32) Cost {
	return ComposeFeasibleP(&m, &c, taxGeneric, delve)
}

// ComposeFeasibleP is composeFeasible over pointers (the offer gate's hot
// path): *c and *m are only read.
func ComposeFeasibleP(m *CostMods, c *Cost, taxGeneric, delve int32) Cost {
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
	m.ApplyTo(&cc)
	cc.Generic = cost.AddClampedGeneric(cc.Generic, int64(taxGeneric))
	if cc.Generic > delve {
		cc.Generic -= delve
	} else {
		cc.Generic = 0
	}
	return cc
}

// FeasibleAny is THE one shared mana-feasibility primitive of the cast flow.
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
func (m CostMods) FeasibleAny(c Cost, pool, snow state.Mana, typed [7]state.Mana, life, taxGeneric, delve int32, bLifeOK bool, rider PipRider, conv *Conv) bool {
	composed := func(c Cost) bool {
		cc := m.ComposeFeasible(c, taxGeneric, delve)
		// The pool-unit floor is a necessary condition of ResolveManaWith's
		// own search (poolUnitsFloor), decided without building its pips.
		if int64(pool.Total()) < cc.PoolUnitsFloor() {
			return false
		}
		_, ok := ResolveManaWith(cc, pool, snow, typed, life, bLifeOK, rider, conv)
		return ok
	}
	if !m.HasFloor() || c.AnnPipCount() == 0 {
		return composed(c)
	}
	var walk func(c Cost) bool
	walk = func(c Cost) bool {
		if c.AnnPipCount() == 0 {
			return composed(c)
		}
		for _, alt := range AnnouncePip(c, 0) {
			r := c
			switch {
			case alt.Color != 0:
				r.Colored[state.ManaIndex(alt.Color)]++
			case alt.Generic > 0:
				r.Generic = cost.AddClampedGeneric(r.Generic, int64(alt.Generic))
			case alt.Life > 0:
				r.Life = cost.AddClampedGeneric(r.Life, int64(alt.Life))
			}
			if walk(r.DropAnnouncePrefix(1)) {
				return true
			}
		}
		return false
	}
	return walk(c)
}

// AnnouncePip resolves the i-th announcement pip of a cost's hybrid →
// monocolour-hybrid → Phyrexian → hybrid-Phyrexian list into its alternative
// payments, in the order manaAsk offers them (each colour, then a generic
// face, then life). It is the single source both manaAsk (the ask's
// valid-option set) and castAnswer (recording the choice) consult, so the
// option offered and the recorded choice always agree. Snow pips are not
// announcement pips: a {S} pip has no alternative payment to announce.
func AnnouncePip(c Cost, i int) []PipAlt {
	if i < len(c.Hybrid) {
		p := c.Hybrid[i]
		return []PipAlt{{Color: p.A}, {Color: p.B}}
	}
	i -= len(c.Hybrid)
	if i < len(c.Twobrid) {
		t := c.Twobrid[i]
		alts := []PipAlt{{Color: t.Col}}
		if t.Generic > 0 {
			alts = append(alts, PipAlt{Generic: t.Generic})
		}
		return alts
	}
	i -= len(c.Twobrid)
	if i < len(c.Phyrexian) {
		letter := c.Phyrexian[i]
		return []PipAlt{{Color: letter}, {Life: 2}}
	}
	i -= len(c.Phyrexian)
	hp := c.HybridPhyrexian[i]
	return []PipAlt{{Color: hp.A}, {Color: hp.B}, {Life: 2}}
}

// PlusRaiseExtra composes a cost with a RaiseCost static's additional parts
// (CostMods.extra). It is Cost.Plus -- the extra carries no mana by
// construction (parseRaiseExtra moves mana and life to the plain raise
// fields) -- followed by the loyalty netting Carth the Lion needs: "loyalty
// abilities cost an additional [+1]" makes a [+1] cost [+2] and a [-2] cost
// [-1] (the card's own ruling), so the source-anchored literal LOYALTY
// add/remove parts merge into ONE net part. The payability gate (a [-N] needs
// N counters) and the settle then read the net cost, exactly as Forge's
// Cost.add merges a CostPutCounter into a CostRemoveCounter of the same type.
func PlusRaiseExtra(c, extra Cost) Cost {
	c = c.Plus(extra)
	for _, part := range extra.AddCounter {
		if strings.EqualFold(part.Spec, "LOYALTY") {
			return NetLoyaltyParts(c)
		}
	}
	return c
}

// NetLoyaltyParts merges every literal, source-anchored LOYALTY AddCounter
// and SubCounter part of c into one net part. An announced or
// target-bearing part (a [-X] ability) is left as written: its count is
// not known until the announcement.
func NetLoyaltyParts(c Cost) Cost {
	literal := func(p cost.CostPart) bool {
		return strings.EqualFold(p.Spec, "LOYALTY") && !p.Announced && p.Dyn == "" && p.Target == ""
	}
	var net int64
	var add, sub []cost.CostPart
	merged := 0
	for _, p := range c.AddCounter {
		if literal(p) {
			net += int64(p.N)
			merged++
			continue
		}
		add = append(add, p)
	}
	for _, p := range c.SubCounter {
		if literal(p) {
			net -= int64(p.N)
			merged++
			continue
		}
		sub = append(sub, p)
	}
	if merged < 2 {
		return c
	}
	if net >= 0 {
		add = append(add, cost.CostPart{N: int32(net), Spec: "LOYALTY"})
	} else {
		sub = append(sub, cost.CostPart{N: int32(-net), Spec: "LOYALTY"})
	}
	c.AddCounter, c.SubCounter = add, sub
	return c
}

// CostModsZero reports whether m is the zero composition in every field
// (TestCostModsZeroCoversEveryField pins the field list).
func CostModsZero(m *CostMods) bool {
	return len(m.Raises) == 0 && !m.HasExtra && m.RaiseCol == (state.Mana{}) && m.RaiseGen == 0 &&
		m.RaiseLife == 0 && len(m.Reduces) == 0 && m.SetFloor == 0 && m.Waterbend == 0 &&
		!m.WaterbendX && m.WaterbendPartX == 0 && m.RaiseX == 0
}
