package pay

import (
	"github.com/adams-shaun/gorge/rules/cost"
	"github.com/adams-shaun/gorge/state"
)

// solve.go is the pool solver (moved from package rules' mana.go and
// mana_convert.go, E7): the pip expansion and the deterministic backtracking
// search that pays a cost's mana and fixed-life parts from a pool and its
// snow/typed tallies, under a payer's riders and colour conversion. Pure.

// ManaLetters is state.Mana's index order (MW, MU, MB, MR, MG, MC) spelled
// out as the WUBRGC symbols events.ManaAdd's Counter field expects.
var ManaLetters = [...]string{"W", "U", "B", "R", "G", "C"}

// Conv is the colour-conversion set one payment is resolved under. The
// zero value converts nothing, which is exactly the behaviour of a game with
// no ManaConvert static on the battlefield -- every pre-existing call site
// degenerates to today's exact-colour match, so no golden game can move
// merely because the machinery exists.
type Conv struct {
	// Wild[i] is true when the payer's mana of colour i (ManaLetters order)
	// may be spent as mana of any COLOUR (CR 608.2i's "any color"): any
	// coloured Pip or hybrid Pip may be paid with it. Colourless-specific
	// {C} pips are NOT covered -- {C} is not a colour (CR 107.4c) -- unless
	// wildC is also set (the AnyType->AnyType wording, "mana of any type").
	Wild [6]bool
	// WildC extends wild to the colourless-specific {C} pips. Set by the
	// AnyType->AnyType conversion ("mana of any type can be spent..."); the
	// corpus carries it on two script lines.
	WildC bool
	// To[i][j] is true when the payer's mana of colour i may be spent as
	// though it were mana of colour j (the White->Red wording). It is a
	// specific mapping, consulted only when the exact-colour match and wild
	// both fail.
	To [6][6]bool
	// OnlyC[i] is the <-C RESTRICTION ("you may spend other mana only as
	// though it were colorless mana", the nonWhite<-C wording, one corpus
	// occurrence): the payer's mana of colour i may pay ONLY a
	// colourless-specific {C} Pip or generic, never a coloured Pip -- not
	// even its own colour's Pip, which is the whole point of the
	// restriction.
	OnlyC [6]bool
}

// Empty reports whether the conversion would change any Pip match. The
// payment sites skip the conversion path entirely on an Empty conv so the
// pure ResolveMana (and with it every pre-existing game) is byte-identical.
func (m *Conv) Empty() bool {
	if m.WildC {
		return false
	}
	for i := range m.Wild {
		if m.Wild[i] || m.OnlyC[i] {
			return false
		}
		for j := range m.To[i] {
			if m.To[i][j] {
				return false
			}
		}
	}
	return true
}

// Pip is one flexible mana demand inside a cost's mana part, as a list of
// alternative payments tried in order. The alternative kinds are exactly the
// mana symbols CR 107.4 knows: one unit of a colour, N generic mana (a
// monocolour hybrid's "2" face), two life (a Phyrexian face), and snow mana
// (a {S} Pip, payable only by a mana a snow permanent produced). A Pip with
// one colour listed twice is just a strict colour Pip.
type Pip struct {
	Alts []PipAlt
}

type PipAlt struct {
	Color   byte  // one unit of this colour (0 = not a colour alternative)
	Generic int32 // this many generic mana (0 = not a generic alternative)
	Life    int32 // two life (0 = not a life alternative)
	Snow    bool  // one snow mana unit
}

// PipRider carries the payer-side may-play riders a cost's Pip alternatives
// expand under: anyColor is MayPlayIgnoreColor$ ("mana of any color",
// CR 401.5), anyType is MayPlayIgnoreType$ ("mana of any type", Rakdos, the
// Muscle). The zero value is the plain exact-colour payment.
type PipRider struct {
	AnyColor bool
	AnyType  bool
}

func expandCostPips(c cost.Cost, dst []Pip, bLifeOK bool, rider PipRider) []Pip {
	// Size the list once: every Pip source below contributes exactly one
	// Pip per unit counted here.
	n := len(c.Hybrid) + len(c.Twobrid) + len(c.Phyrexian) + len(c.HybridPhyrexian)
	if c.Snow > 0 {
		n += int(c.Snow)
	}
	for _, letter := range cost.PipLetters {
		if k := c.Colored[state.ManaIndex(letter)]; k > 0 {
			n += int(k)
		}
	}
	// Built into the caller's buffer when it fits (ResolveManaWith's stack
	// array), so the common small cost allocates no Pip list.
	out := dst[:0]
	if cap(out) < n {
		out = make([]Pip, 0, n)
	}
	// The coloured slots including the colourless one: a plain {C} Pip is a
	// strict colourless requirement generic must not satisfy by stealing the
	// pool's only colourless, so it is reserved like any coloured Pip.
	for _, letter := range cost.PipLetters {
		for n := c.Colored[state.ManaIndex(letter)]; n > 0; n-- {
			// The strict one-colour alternative list is shared read-only
			// (every Pip consumer only ranges alts); it is capped at its
			// length, so the K'rrik append below copies rather than
			// writing into the shared array.
			alts := strictColourAlts[state.ManaIndex(letter)][:1:1]
			if rider.AnyType {
				alts = anyTypeAlts()
			} else if rider.AnyColor && letter != 'C' {
				alts = anyColorAlts()
			}
			if bLifeOK && letter == 'B' {
				alts = append(alts, PipAlt{Life: 2})
			}
			out = append(out, Pip{Alts: alts})
		}
	}
	for _, pair := range c.Hybrid {
		alts := []PipAlt{{Color: pair.A}, {Color: pair.B}}
		if rider.AnyType {
			alts = anyTypeAlts()
		} else if rider.AnyColor {
			alts = anyColorAlts()
		}
		out = append(out, Pip{Alts: alts})
	}
	for _, t := range c.Twobrid {
		var alts []PipAlt
		switch {
		case rider.AnyType:
			alts = anyTypeAlts()
		case rider.AnyColor:
			alts = anyColorAlts()
		default:
			alts = []PipAlt{{Color: t.Col}}
		}
		if t.Generic > 0 {
			alts = append(alts, PipAlt{Generic: t.Generic})
		}
		out = append(out, Pip{Alts: alts})
	}
	for _, letter := range c.Phyrexian {
		alts := []PipAlt{{Color: letter}, {Life: 2}}
		if rider.AnyType {
			alts = append(anyTypeAlts(), PipAlt{Life: 2})
		} else if rider.AnyColor {
			alts = append(anyColorAlts(), PipAlt{Life: 2})
		}
		out = append(out, Pip{Alts: alts})
	}
	for _, hp := range c.HybridPhyrexian {
		alts := []PipAlt{{Color: hp.A}, {Color: hp.B}, {Life: 2}}
		if rider.AnyType {
			alts = append(anyTypeAlts(), PipAlt{Life: 2})
		} else if rider.AnyColor {
			alts = append(anyColorAlts(), PipAlt{Life: 2})
		}
		out = append(out, Pip{Alts: alts})
	}
	for n := c.Snow; n > 0; n-- {
		out = append(out, Pip{Alts: []PipAlt{{Snow: true}}})
	}
	return out
}

// strictColourAlts holds, per mana index, the one-element strict colour
// alternative list costPips hands every plain coloured Pip (read-only).
var strictColourAlts = func() (t [len(cost.PipLetters)][1]PipAlt) {
	for _, letter := range cost.PipLetters {
		t[state.ManaIndex(letter)][0] = PipAlt{Color: letter}
	}
	return t
}()

// anyColorAlts is the colour alternatives a coloured Pip accepts under the
// may-play ignore-colour rider (MayPlayIgnoreColor$ True, CR 401.5): any of
// the five colours, tried in fixed WUBRG order. A {C} Pip never reaches this
// helper: CR 107.4c's "any color" never includes colourless.
func anyColorAlts() []PipAlt {
	return []PipAlt{{Color: 'W'}, {Color: 'U'}, {Color: 'B'}, {Color: 'R'}, {Color: 'G'}}
}

// anyTypeAlts is the MayPlayIgnoreType$ alternative set: ALL six mana types
// (Rakdos, the Muscle's "mana of any type can be spent to cast those spells"
// — "any type" is every mana type, colourless included, unlike "any color"
// which CR 107.4c keeps away from {C}). Used for every Pip kind under the
// anyType rider; coloured pips, {C} pips, hybrids and Phyrexians all widen
// to it.
func anyTypeAlts() []PipAlt {
	return []PipAlt{{Color: 'W'}, {Color: 'U'}, {Color: 'B'}, {Color: 'R'}, {Color: 'G'}, {Color: 'C'}}
}

// Payment is what ResolveMana found: the pool and its two parallel
// tallies after every Pip and the generic requirement were paid, plus any
// Phyrexian face spent. Snow units are always consumed alongside their pool
// slot (Snow[i] never exceeds Pool[i]); typed units (task castfilter2) are
// consumed alongside theirs (TypedMana[k][i] never exceeds Pool[i]).
type Payment struct {
	Pool      state.Mana
	Snow      state.Mana
	Typed     [7]state.Mana
	LifeSpent int32
}

// takeUnit consumes one mana unit from slot i of rem/sn/typed, preferring a
// PLAIN unit when one exists, then the typed units in their fixed
// Treasure > Cave > Desert order, and a SNOW unit LAST so a snow unit stays
// available for a later {S} Pip (typed units have no pips of their own, so
// they go before snow but after plain); the backtracking search undoes the
// choice if the rest of the cost cannot be paid that way. plain is the
// slot's untagged remainder; each tally is <= the pool by construction.
func takeUnit(rem, sn *state.Mana, typed *[7]state.Mana, i int) {
	plain := (*rem)[i] - (*sn)[i]
	for t := range *typed {
		plain -= (*typed)[t][i]
	}
	if plain > 0 {
		(*rem)[i]--
		return
	}
	for t := range *typed {
		if (*typed)[t][i] > 0 {
			(*rem)[i]--
			(*typed)[t][i]--
			return
		}
	}
	(*rem)[i]--
	(*sn)[i]--
}

// ResolveMana finds a concrete payment of the cost's mana and fixed-life
// parts from pool, the pool's parallel snow tally and the payer's life,
// preferring to spend coloured pool mana over life for a Phyrexian Pip and
// the first alternative of each Pip, so the assignment is deterministic. It
// returns the payment with the coloured pips and generic requirement spent,
// and whether the whole cost is payable. The generic requirement is paid
// last from whatever the pips left, so coloured mana is never spent on
// generic while a Pip still needs it; a monocolour hybrid's generic face
// competes in the backtracking search as the Pip's later alternative (its
// generic amount joins the requirement for the rest of the search).
//
// conv, when non-nil, is the stat:ManaConvert conversion set this payment is
// resolved under (rules/mana_convert.go): it WIDENS what a Pip's colour
// alternatives accept -- the payer's converted mana may be spent as though
// it were another colour -- and onlyC may also NARROW it ("spend other mana
// only as though it were colorless"). A nil conv is the plain exact-colour
// match every pre-existing caller keeps, so games with no ManaConvert static
// on the battlefield resolve byte-identically.
func ResolveMana(c cost.Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, conv *Conv) (Payment, bool) {
	return ResolveManaWith(c, pool, snow, typed, life, false, PipRider{}, conv)
}

// ResolveManaWith is ResolveMana with the two payer-side grants applied:
// when bLifeOK is set, every plain {B} Pip additionally accepts 2 life
// (K'rrik, Son of Yawgmoth's "For each {B} in a cost, you may pay 2 life
// rather than pay that mana"); when anyColor is set, every coloured Pip
// (plain, hybrid, twobrid, Phyrexian or hybrid-Phyrexian) is payable by ANY
// colour in the pool -- the may-play grant's MayPlayIgnoreColor$ rider, "you
// may spend mana as though it were mana of any color to cast it" (CR 401.5).
// A {C} Pip stays colourless-only under anyColor: CR 107.4c's "any color"
// never includes colourless. Both grants keep main search's deterministic
// first-alternative preference; the expanded alternatives are tried in fixed
// WUBRG order (see anyColorAlts).
//
// The life parameter is the payer's life total, which may already be 0 or
// less mid-cast (Ancient Tomb's own 2 damage landing between two planned
// activations): state-based actions are not checked until a player would
// receive priority (CR 704.3), and the payer may still finish paying. The
// gate therefore binds only the cost's FIXED life component (c.Life): paying
// N>0 life requires life >= N (CR 119.4), while a cost with no life
// component pays 0 life, which is always legal, whatever the life total. The
// Phyrexian/life Pip alternatives inside the search still require life >= 2
// each, so a dead payer can never pay a Pip with life.
func ResolveManaWith(c cost.Cost, pool, snow state.Mana, typed [7]state.Mana, life int32, bLifeOK bool, rider PipRider, conv *Conv) (Payment, bool) {
	if c.Life > 0 && life < c.Life {
		return Payment{}, false
	}
	var pipBuf [8]Pip
	pips := expandCostPips(c, pipBuf[:0], bLifeOK, rider)
	rem := pool
	sn := snow
	tp := typed
	life -= c.Life
	lifeSpent := c.Life
	// pipExact reports whether col is one of the Pip's colour alternatives
	// (a strict colour Pip lists its colour once; a hybrid lists two).
	pipExact := func(p Pip, col byte) bool {
		for _, alt := range p.Alts {
			if alt.Color == col {
				return true
			}
		}
		return false
	}
	// pipAccepts reports whether one unit of pool colour col (ManaLetters
	// index) may pay Pip p under conv. Exact colours always match; conv
	// widens (wild/to) and narrows (onlyC) around that base. Non-colour
	// alternatives (generic, life, snow) are unaffected by conv.
	pipAccepts := func(p Pip, col byte, di int) bool {
		isC := len(p.Alts) == 1 && p.Alts[0].Color == 'C'
		if conv == nil {
			return pipExact(p, col)
		}
		if conv.OnlyC[di] {
			// The <-C restriction: this pool colour may be spent ONLY as
			// colorless mana -- a {C} Pip or generic (generic is handled
			// outside the Pip loop and takes any mana), never a coloured
			// or hybrid Pip, not even its own colour's.
			return isC
		}
		if pipExact(p, col) {
			return true
		}
		if conv.Wild[di] {
			// "spend as mana of any color" widens to every coloured or
			// hybrid Pip; the colourless-specific {C} Pip is a TYPE, not a
			// colour (CR 107.4c), so it is covered only by the
			// AnyType->AnyType wording (conv.wildC).
			if isC {
				return conv.WildC
			}
			return true
		}
		for _, alt := range p.Alts {
			if alt.Color != 0 && conv.To[di][state.ManaIndex(alt.Color)] {
				return true
			}
		}
		return false
	}
	// finalGeneric is the successful search path's generic requirement: the
	// cost's own Generic plus every monocolour-hybrid Pip that paid its
	// generic face on that path. The closing deduction spends exactly it.
	finalGeneric := c.Generic
	var rec func(i int, generic int32) bool
	rec = func(i int, generic int32) bool {
		if i == len(pips) {
			if rem.Total() < generic {
				return false
			}
			finalGeneric = generic
			return true
		}
		p := pips[i]
		// Try each alternative in order: for a hybrid this prefers A over B;
		// for a single-colour Pip there is one colour alternative, then the
		// Pip's life face for a Phyrexian Pip.
		for _, alt := range p.Alts {
			switch {
			case alt.Color != 0:
				di := state.ManaIndex(alt.Color)
				if rem[di] > 0 && pipAccepts(p, alt.Color, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			case alt.Generic > 0:
				// A monocolour hybrid's generic face: this Pip joins the
				// generic requirement (tried after the colour face, so a
				// colour unit is preferred when the search can still pay).
				if rec(i+1, generic+alt.Generic) {
					return true
				}
			case alt.Life > 0:
				if life >= 2 {
					life -= 2
					lifeSpent += 2
					if rec(i+1, generic) {
						return true
					}
					lifeSpent -= 2
					life += 2
				}
			case alt.Snow:
				// A {S} Pip consumes an actual SNOW unit: both the pool slot
				// and the parallel snow tally, so the unit that leaves is the
				// unit that was snow (never a plain unit misattributed into
				// the tally).
				for di, s := range sn {
					if s > 0 {
						beforeRem, beforeSn, beforeTyped := rem, sn, tp
						rem[di]--
						sn[di]--
						if rec(i+1, generic) {
							return true
						}
						rem, sn, tp = beforeRem, beforeSn, beforeTyped
					}
				}
			}
		}
		// A conversion may let OTHER pool colours pay this Pip too -- e.g.
		// "spend white mana as though it were red" offers the pool's white
		// mana for a red Pip, which the exact-colour alternatives above
		// cannot see. The walk order is ManaLetters (WUBRGC), so the
		// assignment stays deterministic; converted mana is tried only after
		// every exact alternative, so a conversion never displaces an exact
		// payment.
		if conv != nil {
			for di := range ManaLetters {
				col := ManaLetters[di][0]
				if !pipExact(p, col) && rem[di] > 0 && pipAccepts(p, col, di) {
					beforeRem, beforeSn, beforeTyped := rem, sn, tp
					takeUnit(&rem, &sn, &tp, di)
					if rec(i+1, generic) {
						return true
					}
					rem, sn, tp = beforeRem, beforeSn, beforeTyped
				}
			}
		}
		return false
	}
	if !rec(0, c.Generic) {
		return Payment{}, false
	}
	// The search found a Pip assignment that leaves enough total mana; deduct
	// the generic requirement from that remainder, preferring colourless then
	// colours in fixed WUBRG order so payment is deterministic. Generic can
	// be paid by any leftover mana, so a total >= Generic always suffices.
	need := finalGeneric
	for _, i := range [...]int{state.MC, state.MW, state.MU, state.MB, state.MR, state.MG} {
		for need > 0 && rem[i] > 0 {
			takeUnit(&rem, &sn, &tp, i)
			need--
		}
	}
	return Payment{Pool: rem, Snow: sn, Typed: tp, LifeSpent: lifeSpent}, true
}

// payable reports whether the cost's mana and fixed-life parts can be paid
// by pool, its parallel snow tally and the payer's current life (a Phyrexian
// Pip may additionally be paid with two life; a {S} Pip only by snow mana).
// This is the offering gate's feasibility question, and the real answer to
// "is there ANY way this cost can be paid right now" -- the same ResolveMana
// the payment stage uses, so an offered cost and the cost it charges can
// never disagree.
func Payable(c cost.Cost, pool, snow state.Mana, typed [7]state.Mana, life int32) bool {
	_, ok := ResolveMana(c, pool, snow, typed, life, nil)
	return ok
}

func PoolCanPay(c cost.Cost, p state.Mana) bool {
	// Pool-only feasibility, no life and no snow offered: a hybrid must be
	// paid by one of its colours in the pool, a Phyrexian Pip by its colour,
	// a monocolour hybrid by its colour (its generic face is not offered
	// here) and a {S} Pip is unpayable. This is the pure pricing question the
	// corpus invariants ask, and it never treats a hybrid as generic nor lets
	// colourless `pay` it.
	_, ok := ResolveMana(c, p, state.Mana{}, [7]state.Mana{}, 0, nil)
	return ok
}

// Pay spends the cost from a pool and returns what is left. Coloured
// requirements come out first so generic can never strand a colour the cost
// still needs; hybrid pips take one of their pair and Phyrexian pips their
// colour (pool-only -- the cast flow's payMana handles the life half and
// passes a fully-resolved cost here). Mana-only: non-mana parts
// (Tap/Sac/Discard/SubCounter) are the cast flow's own job (rules/cast.go), never
// this function's.
func PoolPay(c cost.Cost, p state.Mana) (state.Mana, bool) {
	// Pool-only: no life and no snow are offered, so a Phyrexian Pip is paid
	// by its colour (the cast flow's payMana handles the life half and passes
	// a fully resolved cost here). ResolveMana already reserves the coloured
	// pips and deducts generic, so the returned pool is fully spent. A failed
	// search returns the input pool untouched.
	pay, ok := ResolveMana(c, p, state.Mana{}, [7]state.Mana{}, 0, nil)
	if !ok {
		return p, false
	}
	return pay.Pool, true
}
