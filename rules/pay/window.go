package pay

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// window.go is the payment-window source vocabulary (lasagna spec W5 E7):
// one permanent's single-tap production alternatives as a payment window
// or the auto-pay planner sees them. The census that builds them from the
// live board stays in package rules (windowManaUnits).

// ManaFreeCost reports whether a mana ability's activation cost is a bare
// tap (or empty -- an ability that produces mana for nothing): Tap may be
// true, but no Sac, no Discard, no SubCounter, no generic/coloured mana, no variable X.
// A cost like "T, Sac <1/CARDNAME>", "T, PayLife<1>", "2 T" or "G T" is
// not free and must not be counted as available-by-tapping.
func ManaFreeCost(c Cost) bool {
	return len(c.Sac) == 0 && len(c.Discard) == 0 && len(c.SubCounter) == 0 &&
		len(c.AddCounter) == 0 && len(c.Exile) == 0 && len(c.Reveal) == 0 &&
		len(c.RevealOrChoose) == 0 &&
		len(c.RevealChosen) == 0 &&
		len(c.Behold) == 0 && len(c.TapPermanent) == 0 && len(c.Blight) == 0 && !c.Forage &&
		c.Generic == 0 && c.Life == 0 && c.Colored == (state.Mana{}) && c.X == 0 &&
		len(c.Hybrid) == 0 && len(c.Phyrexian) == 0
}

// WindowAlt is one deterministic production alternative of a single
// permanent: the exact ability resolveManaAbility will resolve (so
// the activation poses no chooseMana sub-ask), its Produced$ colour counts,
// and its literal amount. A permanent that can tap for one of several
// colours (a Volcanic Island's intrinsic {U} and {R} abilities) carries one
// alt per ability, because the tap yields exactly one of them -- never their
// sum. The window's tap list offers one option per alt, so the payer's
// colour choice is made in the decision rather than in a nested ask.
type WindowAlt struct {
	Ma     *cards.SA
	Counts [6]int32
	Amt    int32
	// Any records ProducedCounts' open-choice result.  Consumers which need a
	// concrete witness (payment plans) must decline it even where the ordinary
	// payment window has a deterministic fallback.
	Any bool
	// Life is the Life the activation pays (a PayLife<N> activation cost).
	// Every alt the shared windowManaUnits builds carries 0; only the
	// cast-payment probe's paid-cost layer sets it, so the affordability
	// search can debit that Life from the payer's budget -- an activation
	// that spends Life must not be promised as if the Life were still
	// available for the cost being priced.
	Life int32
	// CostGeneric is the literal generic mana the activation pays BEFORE the
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
	CostGeneric int32
}

// Mana is the alt's production as a Mana vector, the form the walk's
// affordability search and the window's safety ordering both add to a pool.
func (a WindowAlt) Mana() state.Mana {
	var m state.Mana
	for i, n := range a.Counts {
		m[state.ManaIndex(cards.ManaSymbol(i))] += n * a.Amt
	}
	return m
}

// WindowUnit is one permanent as a PAYMENT WINDOW sees it: its
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
type WindowUnit struct {
	ID        state.ObjID
	FreeCount int
	Alts      []WindowAlt
}

// AppendUnitAlt appends one alternative to the unit that already
// names id, creating the unit if the shared census and the choice-shape layer
// both left it out. It keeps the evaluated-amount layer from duplicating the
// unit-lookup bookkeeping the choice-shape loop spells out inline.
func AppendUnitAlt(units []WindowUnit, id state.ObjID, alt WindowAlt) []WindowUnit {
	for i := range units {
		if units[i].ID == id {
			units[i].Alts = append(units[i].Alts, alt)
			return units
		}
	}
	return append(units, WindowUnit{ID: id, FreeCount: 1, Alts: []WindowAlt{alt}})
}

func PlanAltOK(a WindowAlt) bool {
	if a.Life != 0 || a.Amt <= 0 || a.Ma == nil || a.Ma.API != "Mana" || a.Any {
		return false
	}
	return a.Mana().Total() > 0
}

// SameManaAbility is potentialMembersVerify's comparison (and the live
// paymentPlanCensusOf coverage check): the same ability,
// or (for an ability a grant builds per call) an identical one.
func SameManaAbility(a, b *cards.SA) bool {
	return cards.SAEqual(a, b)
}

// SameUnits compares two source censuses unit by unit and
// alternative by alternative (the verify-mode check of the query cache).
func SameUnits(a, b []WindowUnit) bool {
	return slices.EqualFunc(a, b, func(x, y WindowUnit) bool {
		return x.ID == y.ID && x.FreeCount == y.FreeCount && slices.EqualFunc(x.Alts, y.Alts, SameWindowAlt)
	})
}

// SameWindowAlt is alternative equality up to the identity of an
// ability built per call (a CR 305.6 intrinsic, cards.IntrinsicManaAbility):
// two censuses at one state list the same abilities, but such an ability is
// a fresh pointer each time.
func SameWindowAlt(x, y WindowAlt) bool {
	xa, ya := x, y
	xa.Ma, ya.Ma = nil, nil
	return xa == ya && SameManaAbility(x.Ma, y.Ma)
}

// AltLabel renders an alt's production as a short " for {U}{R}" suffix,
// so a dual land's two abilities are distinguishable on the wire.
func AltLabel(a WindowAlt) string {
	syms := "WUBRGC"
	var b strings.Builder
	for i, n := range a.Counts {
		for k := int32(0); k < n*a.Amt; k++ {
			b.WriteByte(syms[i])
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return " for {" + strings.Join(strings.Split(b.String(), ""), "}{") + "}"
}
